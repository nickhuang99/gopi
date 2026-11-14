"""REAL experiment (not a simulation): measure the pointer-transfer function
counts -> screen px on this box, using XTEST relative motion (the same class of
event a CH9329 relative report produces) on a 1920x1080 X server.
Reset position with XWarpPointer between measurements (reset is not measured)."""
import subprocess, json, sys, os
import Xlib.display, Xlib.X as X
from Xlib.ext import xtest
# Scratch output goes to the shared workspace scratch home (never /tmp — the /
# partition is the 2026-09-11 exhaustion site). The evidence archive keeps the
# collected data next to the other raw outputs of this experiment.
OUT = os.environ.get("GOPI_XTRANSFER_OUT") or os.path.join(
    os.path.dirname(os.path.abspath(__file__)), "xtransfer.json")
disp = sys.argv[1] if len(sys.argv)>1 else ':99'
d = Xlib.display.Display(disp)
root = d.screen().root
START_X, START_Y = 200, 540
def warp():
    root.warp_pointer(START_X, START_Y); d.sync()
def pos():
    p = root.query_pointer(); return p.root_x, p.root_y
def rel(dx, dy):
    xtest.fake_input(d, X.MotionNotify, detail=1, x=dx, y=dy); d.sync()
print("DISPLAY=%s  accel=%s" % (disp, subprocess.run(
    "xset q | grep -A1 'Pointer Control'", shell=True, capture_output=True, text=True).stdout.strip().replace("\n"," ")))
print("%6s %6s %6s %8s" % ("counts","moved","dy","px/count"))
rows=[]
for dx in (1,2,3,4,5,6,8,10,12,14,16,20,24,32,40,48,64,96,127):
    warp(); a=pos(); rel(dx,0); b=pos()
    mv = b[0]-a[0]; rows.append((dx,mv))
    print("%6d %6d %6d %8.3f" % (dx, mv, b[1]-a[1], (mv/dx) if dx else 0))
print("--- large command decompositions (does the servo's own decomposition recover linearity?) ---")
for total in (32,64,127,256,512):
    warp(); a=pos()
    n = -(-total//127)
    step = total//n if n else total
    rest = total
    sent=0
    while rest>0:
        s=min(127,rest); rel(s,0); rest-=s; sent+=1
    b=pos(); print("total=%4d in %d frames -> moved %4d px  ratio=%.3f" % (total,sent,b[0]-a[0],(b[0]-a[0])/total))
json.dump(rows, open(OUT,"w"))
