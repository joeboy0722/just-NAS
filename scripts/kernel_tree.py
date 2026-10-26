"""Read kernel identity from the actual unpacked modloop, never build leftovers."""
import pathlib,re

def kernel_version(tree):
 modules=pathlib.Path(tree)/'modules'
 versions=[p.name for p in modules.iterdir() if p.is_dir() and p.name!='firmware']
 if len(versions)!=1 or not re.fullmatch(r'[0-9][a-zA-Z0-9._+-]*',versions[0]):
  raise ValueError('Expected exactly one kernel version in the actual modloop')
 return versions[0]
