"""Minimal NFSv4.0 READ/WRITE acceptance client (RFC 7530).
Uses AUTH_SYS, the normal anonymous stateid, and a reserved Windows port.
It only accesses the disposable proof.txt on the VM test share.
"""
import os, random, socket, struct, time
HOST = os.environ.get("NAS_TEST_HOST", "127.0.0.1")
PORT = int(os.environ.get("NAS_TEST_NFS_PORT", "32049"))
CLIENT = os.environ.get("NAS_TEST_NFS_CLIENT", "127.0.0.1")
NFS_PATH = os.environ.get("NAS_TEST_NFS_PATH", "/mnt/testpool/files/proof.txt")

class NFSGrace(Exception): pass

def uint(n): return struct.pack("!I", n)
def opaque(b): return uint(len(b)) + b + b"\0" * (-len(b) % 4)

class Reader:
    def __init__(self, b): self.b, self.pos = b, 0
    def u(self):
        value = struct.unpack_from("!I", self.b, self.pos)[0]; self.pos += 4
        return value
    def o(self):
        size = self.u(); result = self.b[self.pos:self.pos+size]
        self.pos += (size+3)//4*4
        return result

def receive(s, count):
    result=b""
    while len(result)<count:
        b=s.recv(count-len(result))
        if not b: raise ConnectionError("Unexpected RPC EOF")
        result+=b
    return result

def exchange_once(operation):
    cred=uint(0)+opaque(b"vmtest")+uint(65534)+uint(65534)+uint(0)
    xid=random.randrange(1,2**31)
    ops=[uint(24)]+[uint(15)+opaque(p) for p in tuple(x.encode() for x in NFS_PATH.strip("/").split("/"))]+[operation]
    body=b"".join(map(uint,[xid,0,2,100003,4,1,1]))+opaque(cred)+uint(0)+uint(0)+opaque(b"vmtest")+uint(0)+uint(len(ops))+b"".join(ops)
    with socket.socket() as s:
        s.settimeout(15)
        for port in random.sample(range(700,1000),32):
            try: s.bind((CLIENT,port)); break
            except OSError: continue
        else: raise RuntimeError("No reserved local source port available")
        s.connect((HOST,PORT));s.sendall(uint(0x80000000|len(body))+body)
        data=b""
        while True:
            marker=struct.unpack("!I",receive(s,4))[0]
            size=marker&0x7fffffff
            assert size<1024*1024, "Oversized RPC record"
            data+=receive(s,size)
            if marker&0x80000000: break
    r=Reader(data)
    assert r.u()==xid and r.u()==1 and r.u()==0, "RPC rejected"
    r.u();r.o();assert r.u()==0,"RPC program rejected"
    status=r.u();r.o();count=r.u()
    if status==10013: raise NFSGrace("NFSv4 server is in reboot recovery grace period")
    assert status==0,("NFS compound failed",status)
    assert count==6
    for code in (24,15,15,15,15):
        assert r.u()==code and r.u()==0,"NFS lookup failed"
    return r

def exchange(operation):
    deadline=time.monotonic()+120
    while True:
        try: return exchange_once(operation)
        except OSError as error:
            if getattr(error,"winerror",None)!=10048 or time.monotonic()>=deadline: raise
            time.sleep(0.1)
        except NFSGrace:
            if time.monotonic()>=deadline: raise
            time.sleep(5)

def read():
    r=exchange(uint(25)+b"\0"*16+struct.pack("!Q",0)+uint(4096))
    assert r.u()==25 and r.u()==0
    r.u()
    return r.o()

def write(data):
    r=exchange(uint(38)+b"\0"*16+struct.pack("!Q",0)+uint(2)+opaque(data))
    assert r.u()==38 and r.u()==0
    assert r.u()==len(data) and r.u()==2,"NFS write was not committed"

if __name__=="__main__":
    original=b"just-nas VM acceptance: original file\n"
    assert read()==original
    changed=b"NFS verified "+b"."*(len(original)-13)
    write(changed);assert read()==changed
    write(original);assert read()==original
    print("PASS: NFSv4 reserved-port read, committed write, and restore")
