import sys,re,collections
import xml.etree.ElementTree as ET
from datetime import datetime
tag,kind=sys.argv[1],sys.argv[2]
mingap=int(sys.argv[3]) if len(sys.argv)>3 else 50
top=int(sys.argv[4]) if len(sys.argv)>4 else 40
start=re.search(r'<start-date>([^<]+)</start-date>',open(f'{tag}.toc.xml').read()).group(1)
t0=datetime.fromisoformat(start).timestamp()
rows=[]
for line in open(f'{tag}.reqlog'):
    m=re.match(r'(\S+) (\d\d:\d\d:\d\d\.\d+).*\[com\.clubmate\.companion\.perf:(frame|step)\] (.*)',line)
    if m: rows.append((datetime.fromisoformat(m.group(1)+'T'+m.group(2)[:12]+start[23:]).timestamp()-t0,m.group(3),m.group(4).strip()))
moves=[]
for i,(t,k,msg) in enumerate(rows):
    if k=='step' and msg.startswith(f'step {kind} begin'):
        e=next((tt for tt,kk,mm in rows[i+1:] if kk=='step' and mm.startswith(f'step {kind} end')),None)
        if e: moves.append((t,e))
wins=[]
for t,k,msg in rows:
    if k!='frame': continue
    gap=int(msg.split()[1])
    if gap<mingap: continue
    for a,z in moves:
        if a<=t<=z+0.1:
            rel=(t-a)/(z-a)
            wins.append((t-gap/1000,t,'start' if rel<0.5 else 'end'))
print('windows',collections.Counter(w[2] for w in wins))
ids={}
def res(el):
    r=el.get('ref'); return ids[r] if r is not None else el
EXCL=('XCTAutomation','UIAccessibility','AXCore','AccessibilityUtilities','libAccessibility')
out={'start':collections.Counter(),'end':collections.Counter()}
tot=collections.Counter()
for ev,el in ET.iterparse(f'{tag}.tp.xml',events=('end',)):
    if el.get('id') is not None: ids[el.get('id')]=el
    if el.tag!='row': continue
    ch=list(el)
    if 'Main Thread' not in (res(ch[1]).get('fmt') or ''): continue
    t=int(res(ch[0]).text)/1e9
    w=next((x for x in wins if x[0]<=t<=x[1]),None)
    if not w: continue
    bt=next((c for c in ch if c.tag in('backtrace','tagged-backtrace')),None)
    if bt is None: continue
    fl=[res(f) for f in res(bt).iter('frame')]
    def bn(f):
        b=f.find('binary'); return res(b).get('name') if b is not None else '?'
    if any(bn(f).startswith(EXCL) for f in fl): continue
    tot[w[2]]+=1
    seen=set()
    for f in fl:
        nm=f.get('name') or '?'
        n=(nm if not nm.startswith('0x') else bn(f))[:110]
        if n in seen: continue
        seen.add(n)
        out[w[2]][n]+=1
for k in ('start','end'):
    n=max(1,sum(1 for w in wins if w[2]==k))
    print(f'== {k}: {n} long frames, {tot[k]/n:.0f} ms main thread per long frame')
    for name,c in out[k].most_common(top):
        print(f'{c/n:6.1f}  {name}')
