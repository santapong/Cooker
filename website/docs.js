'use strict';
// Local documentation navigation and search. No server requests or credentials.
const articles = [...document.querySelectorAll('.doc-article')];
const articleById = new Map(articles.map(article => [article.id, article]));
const sidebar = document.querySelector('#docs-sidebar');
const menu = document.querySelector('#menu-toggle');
const theme = document.querySelector('#theme-toggle');
const toc = document.querySelector('#toc-links');
const dialog = document.querySelector('#search-dialog');
const search = document.querySelector('#docs-search');
const results = document.querySelector('#search-results');
const searchIndex = articles.map(article => ({id:article.id,title:article.dataset.title,summary:article.querySelector('.lead').textContent,text:(article.dataset.keywords + ' ' + article.textContent).toLowerCase()}));
const controls = [...document.querySelectorAll('.js-control')];
function closeMenu(restore = false) {
  sidebar.classList.remove('menu-open');
  document.body.classList.remove('menu-is-open');
  menu.setAttribute('aria-expanded','false');
  document.querySelector('#docs-main').inert = false;
  if (restore) menu.focus();
}
function route(focus = true) {
  let fragment;
  try { fragment = decodeURIComponent(location.hash.slice(1)); } catch { fragment = ''; }
  const [requested, explicitSection] = fragment.split('/');
  const nested = document.getElementById(requested)?.closest('.doc-article');
  const id = articleById.has(requested) ? requested : nested?.id || 'overview';
  const section = explicitSection || (nested && !articleById.has(requested) ? requested : undefined);
  const active = articleById.get(id);
  for (const article of articles) article.hidden = article !== active;
  document.querySelectorAll('[data-doc-link]').forEach(link => {
    if (link.dataset.docLink === id) link.setAttribute('aria-current','page');
    else link.removeAttribute('aria-current');
  });
  document.title = active.dataset.title + ' · Cooker Docs';
  toc.replaceChildren();
  active.querySelectorAll('h2[id]').forEach(heading => {
    const link = document.createElement('a');
    link.href = '#' + id + '/' + heading.id;
    link.textContent = heading.textContent;
    toc.append(link);
  });
  const destination = [...active.querySelectorAll('[id]')].find(node => node.id === section) || active.querySelector('h1');
  closeMenu();
  if (focus) { destination.setAttribute('tabindex','-1'); destination.focus({preventScroll:true}); }
  if (section) destination.scrollIntoView({block:'start'});
  else window.scrollTo({top:0});
}
menu.addEventListener('click', () => {
  const opening = menu.getAttribute('aria-expanded') !== 'true';
  sidebar.classList.toggle('menu-open',opening);
  document.body.classList.toggle('menu-is-open',opening);
  menu.setAttribute('aria-expanded',String(opening));
  document.querySelector('#docs-main').inert = opening;
  if (opening) sidebar.querySelector('[aria-current=page]').focus();
});
window.addEventListener('hashchange',() => route());
window.matchMedia('(min-width:851px)').addEventListener('change', event => { if(event.matches) closeMenu(); });
// Clicking the current route should still close mobile navigation.
document.addEventListener('click',event => {
  const link = event.target.closest('a[href^="#"]');
  if (link && link.getAttribute('href') === location.hash && articleById.has(location.hash.slice(1).split('/')[0])) { event.preventDefault(); route(); }
});
function applyTheme(value) {
  document.documentElement.dataset.docTheme = value;
  theme.setAttribute('aria-label',value === 'dark' ? 'Use light theme' : 'Use dark theme');
}
try { if (localStorage.getItem('cooker-doc-theme') === 'dark') applyTheme('dark'); } catch { /* Preference storage is optional. */ }
theme.addEventListener('click',() => {
  const value = document.documentElement.dataset.docTheme === 'dark' ? 'light' : 'dark';
  applyTheme(value);
  try { localStorage.setItem('cooker-doc-theme',value); } catch { /* Theme still works this visit. */ }
});
function searchDocs() {
  const words = search.value.toLowerCase().trim().split(/\s+/).filter(Boolean);
  const matches = searchIndex.filter(item => words.every(word => item.text.includes(word))).sort((a,b) => {
    const score = item => words.filter(word => item.title.toLowerCase().includes(word)).length;
    return score(b)-score(a);
  });
  results.replaceChildren();
  for (const match of matches) {
    const link = document.createElement('a'); link.className='search-result'; link.href='#'+match.id;
    const title=document.createElement('strong'); title.textContent=match.title;
    const description=document.createElement('span'); description.textContent=match.summary;
    link.append(title,description);
    link.addEventListener('click',() => { dialog.close(); closeMenu(); });
    results.append(link);
  }
  document.querySelector('#search-count').textContent = matches.length ? `${matches.length} ${matches.length === 1 ? 'guide' : 'guides'}${words.length ? ' found' : ' to explore'}` : 'No guides found. Try a broader word, such as “run” or “setup”.';
}
function openSearch() { closeMenu(); searchDocs(); dialog.showModal(); search.focus(); }
document.querySelector('#search-open').addEventListener('click',openSearch);
document.querySelector('#search-close').addEventListener('click',() => dialog.close());
search.addEventListener('input',searchDocs);
dialog.addEventListener('click',event => { if(event.target === dialog) { const r=dialog.getBoundingClientRect(); if(event.clientX<r.left||event.clientX>r.right||event.clientY<r.top||event.clientY>r.bottom) dialog.close(); } });
dialog.addEventListener('keydown',event => {
  if(event.key==='Escape') { event.preventDefault(); dialog.close(); return; }
  const links=[...results.querySelectorAll('a')];
  if(event.key==='ArrowDown'||event.key==='ArrowUp') {
    event.preventDefault(); const index=links.indexOf(document.activeElement);
    if(links.length) links[(index+(event.key==='ArrowDown'?1:-1)+links.length)%links.length].focus();
  }
  if(event.key==='Enter' && document.activeElement===search && links.length) { event.preventDefault(); links[0].click(); }
});
document.addEventListener('keydown',event => {
  if((event.metaKey||event.ctrlKey) && event.key.toLowerCase()==='k') { event.preventDefault(); if(!dialog.open) openSearch(); }
  if(event.key==='Escape' && menu.getAttribute('aria-expanded')==='true') closeMenu(true);
});
for(const button of document.querySelectorAll('[data-copy]')) {
  button.hidden=false;
  button.addEventListener('click',async () => {
    const code=document.getElementById(button.dataset.copy);
    const original=button.textContent;
    try { await navigator.clipboard.writeText(code.textContent); button.textContent='Copied'; document.querySelector('#copy-status').textContent='Command copied.'; }
    catch { const range=document.createRange(); range.selectNodeContents(code); const selection=window.getSelection(); selection.removeAllRanges(); selection.addRange(range); button.textContent='Select & copy'; document.querySelector('#copy-status').textContent='Clipboard unavailable. Command selected; copy it manually.'; }
    setTimeout(() => { button.textContent=original; },2400);
  });
}
// Keep every article readable if JavaScript is unavailable.
document.body.classList.add('docs-enhanced');
controls.forEach(control => { control.hidden=false; });
document.querySelector('.docs-toc').hidden=false;
route(false);
