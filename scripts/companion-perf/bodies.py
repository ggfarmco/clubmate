import sys,re,collections
import xml.etree.ElementTree as ET
from datetime import datetime
EXCL=('XCTAutomation','UIAccessibility','AXCore','AccessibilityUtilities','libAccessibility')
tag,kind=sys.argv[1],sys.argv[2]
start=re.search(r'<start-date>([^<]+)</start-date>',open(tag+'.toc.xml').read()).group(1)
t0=datetime.fromisoformat(start).timestamp()
steps=[]
for line in open(tag+'.reqlog'):
    m=re.match(r'(\S+) (\d\d:\d\d:\d\d\.\d+).*step (\w+) (begin|end) (\S+)',line)
    if m: steps.append((datetime.fromisoformat(m.group(1)+'T'+m.group(2)[:12]+start[23:]).timestamp()-t0,m.group(3),m.group(4)))
ws=[]
for i,(t,k,be) in enumerate(steps):
    if be=='begin' and k==kind:
        e=next((tt for tt,kk,bb in steps[i+1:] if bb=='end' and kk==k),None)
        if e: ws.append((t,e+0.3))
ids={}
def res(el):
    r=el.get('ref'); return ids[r] if r is not None else el
c=collections.Counter()
for ev,el in ET.iterparse(tag+'.tp.xml',events=('end',)):
    if el.get('id') is not None: ids[el.get('id')]=el
    if el.tag!='row': continue
    ch=list(el)
    if 'Main Thread' not in (res(ch[1]).get('fmt') or ''): continue
    t=int(res(ch[0]).text)/1e9
    if not any(a<=t<=z for a,z in ws): continue
    bt=next((x for x in ch if x.tag in('backtrace','tagged-backtrace')),None)
    if bt is None: continue
    fl=[res(f) for f in res(bt).iter('frame')]
    def bn(f):
        b=f.find('binary'); return res(b).get('name') if b is not None else '?'
    if any(bn(f).startswith(EXCL) for f in fl): continue
    types=set()
    for f in fl:
        n=f.get('name') or ''
        m=re.match(r'(?:specialized |closure #\d+ in )*([A-Z][A-Za-z0-9_.]*?)\.body(?:\.getter|\(content:\))',n)
        if m and bn(f)=='Companion': types.add(m.group(1))
    for ty in types: c[ty]+=1
print(f'{tag}:{kind} windows={len(ws)}  (ms per move, sampled inclusive)')
print(', '.join(f'{k} {v/len(ws):.1f}' for k,v in c.most_common(40)))
