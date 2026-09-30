() => {
  // Tool-owned live node references, valid only in this document.
  let cache = window.__cyberObservation;
  if (!cache) {
    cache = {document: globalThis.crypto?.randomUUID?.() || String(Date.now())+'-'+Math.random().toString(36).slice(2), revision: 0, ids: new WeakMap(), nodes: new Map(), next: 0};
    cache.observer = new MutationObserver(records => { cache.revision += records.length; });
    cache.observer.observe(document, {subtree:true,childList:true,attributes:true,characterData:true});
    Object.defineProperty(window, '__cyberObservation', {value: cache, configurable: true});
  }
  cache.revision += cache.observer.takeRecords().length;
  const visible = e => {
    const r = e.getBoundingClientRect();
    return r.width > 0 && r.height > 0 && r.bottom > 0 && r.right > 0 && r.top < innerHeight && r.left < innerWidth &&
      e.checkVisibility({checkOpacity:true, checkVisibilityCSS:true});
  };
  const selector = e => {
    if (e.id && document.querySelectorAll('#'+CSS.escape(e.id)).length === 1) return '#'+CSS.escape(e.id);
    const parts = [];
    for (let n=e; n && n.nodeType===1; n=n.parentElement) {
      let p=n.localName;
      const siblings=n.parentElement ? [...n.parentElement.children].filter(x=>x.localName===n.localName) : [];
      if (siblings.length>1) p+=':nth-of-type('+(siblings.indexOf(n)+1)+')';
      parts.unshift(p);
    }
    return parts.join(' > ');
  };
  const elements=[];
  for (const e of document.querySelectorAll('button,a[href],input,textarea,select,[role=button],[role=checkbox],[role=radio],[role=option],[role=combobox]')) {
    if (!visible(e)) continue;
    let node=cache.ids.get(e);
    if (!node) {node=++cache.next;cache.ids.set(e,node);cache.nodes.set(node,e);}
    const label=e.getAttribute('aria-label') || [...(e.labels||[])].map(x=>x.innerText).join(' ') || e.innerText || e.getAttribute('placeholder') || e.getAttribute('name') || '';
    const sensitive=e.matches('input[type=password]') || /password|token|secret|otp/i.test(e.getAttribute('name')||'');
    elements.push({node,selector:selector(e),label:label.slice(0,240),tag:e.localName,type:e.getAttribute('type')||'',
      role:e.getAttribute('role')||'',value:sensitive?'[REDACTED]':String(e.value||'').slice(0,1000),sensitive,
      checked:!!e.checked,readonly:!!e.readOnly,expanded:e.getAttribute('aria-expanded'),
      disabled:e.matches(':disabled')||!!e.closest('[inert],[aria-disabled=true]'),required:!!e.required||e.getAttribute('aria-required')==='true',invalid:e.matches(':invalid')||e.getAttribute('aria-invalid')==='true',
      options:e.localName==='select' ? [...e.options].map(o=>({value:o.value,label:o.text,selected:o.selected,disabled:o.disabled||!!o.closest('optgroup[disabled]')})) : []});
    if(elements.length>64)break;
  }
  for (const [id,e] of cache.nodes) if(!e.isConnected)cache.nodes.delete(id);
  const text=[];const walker=document.createTreeWalker(document.body,NodeFilter.SHOW_TEXT);
  let n,size=0;
  while((n=walker.nextNode())){const p=n.parentElement;if(!p||p.closest('script,style,noscript')||!visible(p))continue;const t=n.textContent.trim();if(t){text.push(t);size+=t.length;if(size>8000)break;}}
  return {document:cache.document,revision:cache.revision,url:location.href,title:document.title,text:text.join('\n'),elements,
    scroll:{x:scrollX,y:scrollY,height:document.documentElement.scrollHeight,viewport:innerHeight,width:innerWidth},loading:document.readyState!=='complete'};
}
