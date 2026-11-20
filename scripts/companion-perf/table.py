import sys,re,collections
import xml.etree.ElementTree as ET
from datetime import datetime
KEYS=[('SessionDetailView.body','SessionDetailView.body.getter'),('SessionDetailView.screen','SessionDetailView.screen.getter'),('SessionScreen.init','SessionScreen.init('),
('MarkdownView.body','MarkdownView.body.getter'),('Markdown.inline','static Markdown.inline(_:docLinks'),('Markdown.blocks','static Markdown.blocks('),('Regex.init','Regex.init('),
('CoreText typeset','CTTypesetterCreateWithAttributedStringAndOptions'),('ComposerTextView.sizeThatFits','ComposerTextView.sizeThatFits'),('CappedWidth.sizeThatFits','CappedWidth.sizeThatFits'),
('SessionListView.body','SessionListView.body.getter'),('SessionListView.tree','SessionListView.tree.getter'),('SessionListView.rows','SessionListView.rows('),
('AgentsStore.loadSessions','AgentsStore.loadSessions()'),('RunRows.sessions','static RunRows.sessions('),('JSONValue.decode','JSONValue.decode<'),('ListCache.save','ListCache.save<'),
('AgentsStore.loadTranscript','AgentsStore.loadTranscript('),('AgentTranscript ==','AgentTranscript.=='),
('AppNavigation.stacks.modify','AppNavigation.stacks.modify'),('TabStacks.save','TabStacks.save('),('TabRoot.body','TabRoot.body.getter'),('RootView.body','RootView.body.getter'),
('DocumentController.init','DocumentController.init(kind:)'),('WKWebView init','DocumentWKWebView.init(frame'),
('AutoLayout NSISEngine','-[NSISEngine withBehaviors:performModifications:]'),('CA commit','CA::Transaction::commit()'),('UIView layout','-[UIView(CALayerDelegate) layoutSublayersOfLayer:]')]
EXCL=('XCTAutomation','UIAccessibility','AXCore','AccessibilityUtilities','libAccessibility')
def run(tp,toc,reqlog,kinds):
    start=re.search(r'<start-date>([^<]+)</start-date>',open(toc).read()).group(1)
    t0=datetime.fromisoformat(start).timestamp()
    steps=[]
    for line in open(reqlog):
        m=re.match(r'(\S+) (\d\d:\d\d:\d\d\.\d+).*step (\w+) (begin|end) (\S+)',line)
        if m: steps.append((datetime.fromisoformat(m.group(1)+'T'+m.group(2)[:12]+start[23:]).timestamp()-t0,m.group(3),m.group(4)))
    ws={k:[] for k in kinds}
    for i,(t,k,be) in enumerate(steps):
        if be=='begin' and k in ws:
            e=next((tt for tt,kk,bb in steps[i+1:] if bb=='end' and kk==k),None)
            if e: ws[k].append((t,e+0.3))
    ids={}
    def res(el):
        r=el.get('ref'); return ids[r] if r is not None else el
    out={k:collections.Counter() for k in kinds}
    for ev,el in ET.iterparse(tp,events=('end',)):
        if el.get('id') is not None: ids[el.get('id')]=el
        if el.tag!='row': continue
        ch=list(el)
        if 'Main Thread' not in (res(ch[1]).get('fmt') or ''): continue
        t=int(res(ch[0]).text)/1e9
        hit=[k for k in kinds if any(a<=t<=z for a,z in ws[k])]
        if not hit: continue
        bt=next((c for c in ch if c.tag in('backtrace','tagged-backtrace')),None)
        if bt is None: continue
        fl=[res(f) for f in res(bt).iter('frame')]
        def bn(f):
            b=f.find('binary'); return res(b).get('name') if b is not None else '?'
        if any(bn(f).startswith(EXCL) for f in fl): continue
        names=[f.get('name') or '' for f in fl]
        for k in hit:
            out[k]['TOTAL']+=1
            for label,pat in KEYS:
                if any(pat in n for n in names): out[k][label]+=1
    for k in kinds: out[k]['_n']=len(ws[k])
    return out
cols={}
for spec in sys.argv[1:]:
    tag,kinds=spec.split(':'); kinds=kinds.split(',')
    r=run(f'{tag}.tp.xml',f'{tag}.toc.xml',f'{tag}.reqlog',kinds)
    for k in kinds: cols[f'{k}({tag})']=r[k]
names=list(cols)
print('| ms per move | '+' | '.join(names)+' |'); print('|'+'---|'*(len(names)+1))
for label in ['TOTAL']+[l for l,_ in KEYS]:
    print(f'| {label} | '+' | '.join(f'{cols[c][label]/max(cols[c]["_n"],1):.0f}' for c in names)+' |')
