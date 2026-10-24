from http.server import ThreadingHTTPServer,BaseHTTPRequestHandler
from pathlib import Path
import requests,json,argparse
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
  if args.vm_assets or self.path.startswith(('/api/','/files-api/')):
   headers={k:v for k,v in self.headers.items() if k.lower() not in ['host','origin','referer','content-length','connection','accept-encoding']}
   if self.command!='GET':headers['Origin']=BASE;headers['Referer']=BASE+'/'
   body=self.rfile.read(int(self.headers.get('Content-Length','0')))
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
