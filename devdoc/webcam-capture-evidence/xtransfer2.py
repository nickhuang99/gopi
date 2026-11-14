"""REAL experiment: does X11 pointer acceleration apply to XTEST relative motion?
A/B the same sweep with acceleration ON vs OFF, and reproduce the earlier anomaly."""
import subprocess, sys
import Xlib.display, Xlib.X as X
from Xlib.ext import xtest
d=Xlib.display.Display(sys.argv[1] if len(sys.argv)>1 else ':99')
root=d.screen().root
def sh(c): return subprocess.run(c,shell=True,capture_output=True,text=True).stdout.strip()
def pos(): p=root.query_pointer(); return p.root_x,p.root_y
def rel(dx,dy=0): xtest.fake_input(d,X.MotionNotify,detail=1,x=dx,y=dy); d.sync()
def warp(x=200,y=540): root.warp_pointer(x,y); d.sync()
for label,cmd in (("accel 2/1 thr 4 (default)","xset m 2/1 4"),("accel 1/1 thr 0 (OFF)","xset m 1/1 0")):
    sh(cmd); print("=== %s -> %s"%(label, sh("xset q | grep -A1 'Pointer Control' | tail -1")))
    out=[]
    for dx in (1,2,3,4,5,6,8,12,16,24,32,64,127):
        warp(); a=pos(); rel(dx); b=pos(); out.append("%d:%d"%(dx,b[0]-a[0]))
    print("   reset-then-move  counts:moved  ", " ".join(out))
    # no reset between steps (the earlier anomaly path), walking right
    warp(200,540); seq=[]; a=pos()
    for dx in (1,2,3,5,6,8,12,16,24,48,96,127,255,508):
        a=pos(); rel(dx); b=pos(); seq.append("%d:%d"%(dx,b[0]-a[0]))
    print("   consecutive steps counts:moved ", " ".join(seq), " final x=",pos()[0])
sh("xset m 2/1 4")
