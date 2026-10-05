//go:build full

package playwright

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-rod/rod"
)

// A fixed host reader, never a generated page-evaluation program. Open shadow
// roots are addressable via shadow=[host selector,...,element selector]. Closed
// shadow roots and cross-origin frames are explicit unsupported boundaries.
const structuredDOMReader = `() => {
 const rows=[],limit=256;let truncated=false;
 const path=e=>{const a=[];while(e&&e.nodeType===1){let n=1;for(let p=e.previousElementSibling;p;p=p.previousElementSibling)if(p.localName===e.localName)n++;a.unshift(e.localName+':nth-of-type('+n+')');e=e.parentElement;}return a.join(' > ');};
 const text=e=>(e.innerText||e.textContent||'').trim().slice(0,512);
 function walk(root,hosts){for(const e of root.querySelectorAll('*')){
  if(rows.length>=limit){truncated=true;return;}
  const tag=e.localName,role=e.getAttribute('role')||'',control=/^(input|textarea|button|select|a|option)$/.test(tag);
  if(control||role||/^(p|h[1-6]|output|li|td|th|label|iframe)$/.test(tag)){
   const selector=path(e),address=hosts.length?'shadow='+JSON.stringify(hosts.concat(selector)):selector;
   const r=e.getBoundingClientRect(),style=getComputedStyle(e);
   rows.push({address,tag,role,label:e.getAttribute('aria-label')||(e.labels?Array.from(e.labels).map(text).join(' '):'')||e.getAttribute('placeholder')||'',text:text(e),value:'value' in e?e.value:null,type:e.getAttribute('type'),name:e.getAttribute('name'),href:e.getAttribute('href'),visible:r.width>0&&r.height>0&&style.visibility!=='hidden'&&style.display!=='none',disabled:!!e.disabled,checked:'checked' in e?!!e.checked:null,frame:tag==='iframe'});
  }
  if(e.shadowRoot)walk(e.shadowRoot,hosts.concat(path(e)));
 }}
 walk(document,[]);
 return {url:location.href,title:document.title,text:(document.body?document.body.innerText:'').slice(0,8192),elements:rows,truncated,boundaries:['closed shadow roots','cross-origin frames','popups','downloads']};
}`

func structuredSnapshot(page *rod.Page, session string) (string, error) {
	value, err := page.Eval(structuredDOMReader)
	if err != nil {
		return "", err
	}
	var data map[string]any
	if err = json.Unmarshal([]byte(value.Value.JSON("", "")), &data); err != nil {
		return "", err
	}
	data["session"], data["format"] = session, "playwright-dom/1"
	encoded, err := json.Marshal(data)
	return string(encoded), err
}
func shadowElement(page *rod.Page, address string) (*rod.Element, error) {
	var paths []string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(address, "shadow=")), &paths); err != nil || len(paths) < 2 || len(paths) > 16 {
		return nil, fmt.Errorf("invalid shadow address")
	}
	return page.ElementByJS(rod.Eval(`paths=>{let root=document,e;for(let i=0;i<paths.length;i++){e=root.querySelector(paths[i]);if(!e)return null;if(i+1<paths.length){root=e.shadowRoot;if(!root)return null;}}return e;}`, paths))
}
