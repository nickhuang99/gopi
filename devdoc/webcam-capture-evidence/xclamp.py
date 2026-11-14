"""ROOT CAUSE of the 'nonlinear transfer' anomaly: reproduce the original
consecutive-walk on a 1280-wide display and print x after every step."""
import Xlib.display, Xlib.X as X
from Xlib.ext import xtest
import sys
d=Xlib.display.Display(sys.argv[1]); root=d.screen().root
def pos(): p=root.query_pointer(); return p.root_x,p.root_y
def rel(dx): xtest.fake_input(d,X.MotionNotify,detail=1,x=dx,y=0); d.sync()
root.warp_pointer(200,65); d.sync()
print("display=%s start=%s"%(sys.argv[1],pos()))
for dx in (1,2,3,5,8,12,20,32,64,127,255,508):
    a=pos(); rel(dx); b=pos()
    print("  dx=%4d  x: %4d -> %4d  moved=%4d  ratio=%.3f"%(dx,a[0],b[0],b[0]-a[0],(b[0]-a[0])/dx))
