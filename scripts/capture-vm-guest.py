"""Capture QEMU's actual framebuffer through the authenticated app API."""
import importlib.util,pathlib,sys,json,io
from PIL import Image
R=pathlib.Path(__file__).resolve().parents[1];sp=importlib.util.spec_from_file_location('v',R/'scripts/verify-vm-app.py');v=importlib.util.module_from_spec(sp);sp.loader.exec_module(v);v.ready()
name=sys.argv[1];m=next(m for m in v.call('app-services/vm-manager/vms')['vms'] if name.lower() in m['name'].lower())
r=v.s.post(v.BASE+'/api/app-services/vm-manager/vms/'+m['id']+'/screenshot',timeout=30);r.raise_for_status()
target=R/'output/vm-manager-review'/('guest-'+name.lower().replace(' ','-')+'.png');Image.open(io.BytesIO(r.content)).save(target);print(target)
