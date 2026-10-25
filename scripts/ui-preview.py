from http.server import ThreadingHTTPServer,BaseHTTPRequestHandler
from pathlib import Path
import requests,json,argparse,socket,ssl,select
parser=argparse.ArgumentParser(description="Loopback-only UI test server with certificate-verified VM API proxy")
parser.add_argument("--port",type=int,default=8765)
parser.add_argument("--vm-assets",action="store_true",help="Serve VM embedded assets instead of local web sources")
args=parser.parse_args()
ROOT=Path(__file__).resolve().parents[1]
BASE='https://192.168.56.101:8443'
CERT=str(ROOT/'output/vm-test/just-nas-certificate.pem')
class Handler(BaseHTTPRequestHandler):
 def do_GET(self): self.run_request()
 def do_POST(self): self.run_request()
 def do_PATCH(self): self.run_request()
 def do_PUT(self): self.run_request()
 def do_DELETE(self): self.run_request()
 def run_request(self):
  if self.headers.get('Upgrade','').lower()=='websocket':
   # Pin the real NAS certificate; do not disable TLS verification in Chrome.
   upstream=ssl.create_default_context(cafile=CERT).wrap_socket(socket.create_connection(('192.168.56.101',8443),timeout=15),server_hostname='192.168.56.101')
   try:
    headers={k:v for k,v in self.headers.items() if k.lower() not in ['host','origin','referer']};headers['Host']='192.168.56.101:8443';headers['Origin']=BASE
    raw='GET '+self.path+' HTTP/1.1\r\n'+''.join(k+': '+v+'\r\n' for k,v in headers.items())+'\r\n';upstream.sendall(raw.encode('latin-1'))
    response=b''
    while not response.endswith(b'\r\n\r\n') and len(response)<65536:
     part=upstream.recv(1)
     if not part:break
     response+=part
    self.connection.sendall(response);self.close_connection=True
    if not response.startswith(b'HTTP/1.1 101'):return
    upstream.settimeout(None);self.connection.settimeout(None)
    while True:
     ready,_,_=select.select([upstream,self.connection],[],[],30)
     for source in ready:
      data=source.recv(65536)
      if not data:return
      (self.connection if source is upstream else upstream).sendall(data)
   finally:upstream.close()
   return
  if args.vm_assets or self.path.startswith(('/api/','/files-api/')):
   headers={k:v for k,v in self.headers.items() if k.lower() not in ['host','origin','referer','content-length','connection','accept-encoding']}
   if self.command!='GET':headers['Origin']=BASE;headers['Referer']=BASE+'/'
   size=int(self.headers.get('Content-Length','0'))
   class StreamingBody:
    def __len__(stream):return size
    def __iter__(stream):
     remaining=size
     while remaining:
      data=self.rfile.read(min(1024*1024,remaining))
      if not data:raise IOError('Client upload interrupted')
      remaining-=len(data);yield data
   body=StreamingBody() if size>1024*1024 else self.rfile.read(size)
   try:r=requests.request(self.command,BASE+self.path,headers=headers,data=body,verify=CERT,timeout=115)
   except Exception as e:self.send_error(502,str(e));return
   self.send_response(r.status_code)
   for k,v in r.headers.items():
    if k.lower() in ['content-type','set-cookie','content-disposition']:
     if k.lower()=='set-cookie':v=v.replace('; Secure','')
     self.send_header(k,v)
   self.end_headers();self.wfile.write(r.content)
  else:
   path=ROOT/'web'/self.path.split('?')[0].lstrip('/')
   if path==ROOT/'web':path=path/'index.html'
   if not path.resolve().is_relative_to(ROOT/'web') or not path.is_file():self.send_error(404);return
   self.send_response(200);self.send_header('Content-Type',{'.js':'application/javascript','.css':'text/css','.html':'text/html'}.get(path.suffix,'application/octet-stream')+'; charset=utf-8');self.end_headers();self.wfile.write(path.read_bytes())
 def log_message(self,*args):pass
print(f'UI test preview on http://127.0.0.1:{args.port} (VM TLS certificate verified)',flush=True)
ThreadingHTTPServer(('127.0.0.1',args.port),Handler).serve_forever()
