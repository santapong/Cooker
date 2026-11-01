'use strict';
// Local illustrative state only. No Cooker or provider API is called.
const menuButton = document.querySelector('.menu-toggle');
const mobileMenu = document.querySelector('#mobile-nav');
function closeMenu(restoreFocus = false) {
  menuButton.setAttribute('aria-expanded', 'false');
  menuButton.setAttribute('aria-label', 'Open navigation');
  menuButton.querySelector('use').setAttribute('href', '#i-menu');
  mobileMenu.hidden = true;
  if (restoreFocus) menuButton.focus();
}
menuButton.addEventListener('click', () => {
  if (menuButton.getAttribute('aria-expanded') === 'true') return closeMenu();
  mobileMenu.hidden = false;
  menuButton.setAttribute('aria-expanded', 'true');
  menuButton.setAttribute('aria-label', 'Close navigation');
  menuButton.querySelector('use').setAttribute('href', '#i-close');
});
mobileMenu.addEventListener('click', event => { if (event.target.closest('a')) closeMenu(); });
document.addEventListener('keydown', event => { if (event.key === 'Escape' && !mobileMenu.hidden) closeMenu(true); });
window.matchMedia('(min-width: 801px)').addEventListener('change', event => { if (event.matches) closeMenu(); });

const examples = {
  release: {
    name: 'A reviewed release, with parallel checks.', summary: 'Push waits for both checks; deployment waits for review.',
    selected: 'push', guide: 'docs.html#concepts',
    nodes: [
      { id:'source', name:'Source', type:'SOURCE', x:24, y:144, symbol:'⌘', kitchen:'THE RECIPE', text:'Start with the repository revision for this example. A reviewed Compose App separately pins its saved source to an exact commit.', waits:[] },
      { id:'build', name:'Build image', type:'BUILD', x:210, y:144, symbol:'◇', kitchen:'PREPARATION', text:'Build the container image. Both check branches depend on a successful build.', waits:['source'] },
      { id:'unit', name:'Unit tests', type:'TEST', x:402, y:44, symbol:'↗', kitchen:'ONE STATION', text:'Run the configured test command. This branch can proceed independently of the smoke check after Build succeeds.', waits:['build'] },
      { id:'smoke', name:'Smoke check', type:'TEST', x:402, y:244, symbol:'↘', kitchen:'ANOTHER STATION', text:'Run a separate configured check against the built image. Push must also wait for this branch to succeed.', waits:['build'] },
      { id:'push', name:'Push image', type:'PUSH', x:600, y:144, symbol:'↑', kitchen:'THE PLATING STATION', text:'Both test branches must succeed before the image is published to the configured registry. This is an all-prerequisites join, not a choice between branches.', waits:['unit','smoke'] },
      { id:'review', name:'Review', type:'APPROVAL', x:800, y:144, symbol:'◇', kitchen:'THE PASS', text:'A human review gate follows publication. Adding an approval stage and actually recording approval are separate actions.', waits:['push'] },
      { id:'deploy', name:'Deploy', type:'DEPLOY', x:1000, y:144, symbol:'↗', kitchen:'SERVICE', text:'After the required approval, the configured adapter handles deployment. A completed stage and ongoing application health are separate observations.', waits:['review'] },
    ],
    edges:[['source','build','M172 182H210'],['build','unit','M358 170H378V82H402'],['build','smoke','M358 194H378V282H402'],['unit','push','M550 82H574V170H600'],['smoke','push','M550 282H584V194H600'],['push','review','M748 182H800'],['review','deploy','M948 182H1000']],
  },
  n8n: {
    name:'An n8n stack, with explicit startup requirements.', summary:'Worker has three prerequisites: PostgreSQL, Redis and main.',
    selected:'worker', guide:'workspace.html#example/n8n',
    nodes:[
      { id:'postgres', name:'PostgreSQL', type:'SERVICE', x:120, y:44, symbol:'▱', kitchen:'A REQUIRED INGREDIENT', text:'The database is a prerequisite for both main and worker. Readiness comes from the example Compose health condition, not from this picture.', waits:[] },
      { id:'redis', name:'Redis', type:'SERVICE', x:120, y:244, symbol:'▱', kitchen:'ANOTHER REQUIRED INGREDIENT', text:'The queue service is a prerequisite for both main and worker. The example uses existing images rather than Build and Push stages.', waits:[] },
      { id:'main', name:'n8n main', type:'SERVICE', x:510, y:144, symbol:'◇', kitchen:'THE MAIN STATION', text:'Main starts after the PostgreSQL and Redis requirements in the Compose example are met. These arrows describe startup order, not runtime request traffic.', waits:['postgres','redis'] },
      { id:'worker', name:'n8n worker', type:'SERVICE', x:960, y:144, symbol:'↗', kitchen:'THE JOIN', text:'Worker waits for all three incoming requirements: PostgreSQL, Redis and main. Crossing lines do not create extra dependencies. This preview does not start any service.', waits:['postgres','redis','main'] },
    ],
    edges:[['postgres','main','M268 92H430V168H510'],['redis','main','M268 280H450V196H510'],['postgres','worker','M268 62H880V158H960'],['redis','worker','M268 310H900V210H960'],['main','worker','M658 182H960']],
  },
};
const graph = document.querySelector('#recipe-graph');
const tabs = [...document.querySelectorAll('[data-example]')];
let activeExample = 'release';
function selectStep(id) {
  const example = examples[activeExample];
  const node = example.nodes.find(item => item.id === id);
  if (!node) return;
  example.selected = id;
  graph.querySelectorAll('.recipe-node').forEach(button => button.setAttribute('aria-pressed',String(button.dataset.step === id)));
  graph.querySelectorAll('[data-edge-target]').forEach(path => path.classList.toggle('selected-edge',path.dataset.edgeTarget === id));
  document.querySelector('#step-kind').textContent = `${node.type} / ${node.kitchen}`;
  document.querySelector('#step-title').textContent = node.name;
  document.querySelector('#step-description').textContent = node.text;
  document.querySelector('#step-requires').textContent = node.waits.length ? `Waits for all: ${node.waits.map(key => example.nodes.find(n => n.id === key).name).join(' + ')}` : 'No incoming requirements in this example.';
  const guide = document.querySelector('#step-guide'); guide.href = example.guide;
  guide.textContent = activeExample === 'release' ? 'Read the pipeline concepts ↗' : 'Inspect the n8n example ↗';
}
function setExample(key) {
  const example = examples[key]; if (!example) return;
  activeExample = key;
  tabs.forEach(tab => { const selected = tab.dataset.example === key; tab.setAttribute('aria-selected',String(selected)); tab.tabIndex = selected ? 0 : -1; });
  document.querySelector('#recipe-panel').setAttribute('aria-labelledby',`example-${key}`);
  document.querySelector('#recipe-name').textContent = example.name;
  document.querySelector('#recipe-summary').textContent = example.summary;
  // Fixed, project-owned strings, never account/user input.
  graph.innerHTML = `<svg class="recipe-edges" viewBox="0 0 1200 360" aria-hidden="true"><defs><marker id="recipe-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M1 1L7 4L1 7" style="fill:none;stroke:#a3a39c;stroke-width:1.2"/></marker></defs>${example.edges.map(([source,target,path]) => `<path d="${path}" data-edge-target="${target}" marker-end="url(#recipe-arrow)"/>`).join('')}</svg>${example.nodes.map(node => `<button class="recipe-node" type="button" data-step="${node.id}" aria-pressed="false" style="left:${node.x}px;top:${node.y}px"><small>${node.type}</small><strong>${node.name}</strong><span class="recipe-symbol" aria-hidden="true">${node.symbol}</span></button>`).join('')}`;
  graph.querySelectorAll('[data-step]').forEach(button => button.addEventListener('click',() => selectStep(button.dataset.step)));
  graph.parentElement.scrollLeft = 0; selectStep(example.selected);
}
tabs.forEach((tab,index) => {
  tab.addEventListener('click',() => setExample(tab.dataset.example));
  tab.addEventListener('keydown',event => {
    let target;
    if (event.key === 'ArrowRight') target = (index+1)%tabs.length;
    else if (event.key === 'ArrowLeft') target = (index+tabs.length-1)%tabs.length;
    else if (event.key === 'Home') target = 0;
    else if (event.key === 'End') target = tabs.length-1;
    else return;
    event.preventDefault(); setExample(tabs[target].dataset.example); tabs[target].focus();
  });
});
setExample('release');
const copyButton = document.querySelector('#copy-command');
copyButton.addEventListener('click',async () => {
  const code = document.querySelector('#setup-command'); const status = document.querySelector('#copy-status');
  copyButton.disabled = true;
  try {
    if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
    await navigator.clipboard.writeText(code.textContent);
    copyButton.querySelector('span').textContent = 'Copied'; status.textContent = 'Commands copied. Paste them into your terminal when ready.';
  } catch {
    const range = document.createRange(); range.selectNodeContents(code);
    const selection = window.getSelection(); selection.removeAllRanges(); selection.addRange(range);
    code.parentElement.focus(); status.textContent = 'Clipboard unavailable. Commands selected—use your browser’s Copy action.';
  } finally { copyButton.disabled = false; }
});
