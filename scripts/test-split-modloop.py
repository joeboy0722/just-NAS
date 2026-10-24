import importlib.util,unittest
spec=importlib.util.spec_from_file_location('splitmod','scripts/split-modloop.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class Boundaries(unittest.TestCase):
 def test_core(self):
  for n in ['modules/firmware/intel/ice/ddp/ice.pkg.zst','modules/firmware/amd/amd_sev.bin.zst','modules/firmware/brcm/wifi.bin','modules/6/kernel/drivers/net/ethernet/nvidia/forcedeth.ko','modules/6/kernel/drivers/usb/storage/uas.ko','modules/6/kernel/drivers/gpu/drm/ast/ast.ko','modules/6/kernel/drivers/gpu/drm/vmwgfx/vmwgfx.ko','modules/6/extra/zfs.ko']:
   self.assertIsNone(m.classify(n),n)
 def test_optional(self):
  for n in ['modules/firmware/nvidia/x','modules/firmware/intel/vpu/x','modules/6/kernel/drivers/media/usb/uvc.ko','modules/6/kernel/sound/core/snd.ko','modules/6/kernel/drivers/gpu/drm/nouveau/nouveau.ko']:
   self.assertIsNotNone(m.classify(n),n)
if __name__=='__main__':unittest.main()
