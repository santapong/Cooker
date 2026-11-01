'use strict';
// An authored, in-memory pipeline example. No graph is sent to the Cooker server.
const workspaceDag = (() => {
  const kinds = {build:'Build',test:'Test',push:'Push',approval:'Approval',deploy:'Deploy',custom:'Custom'};
  const symbols = {approval:'check',custom:'sliders'};
  const explanations = {
    build:'Turn the reviewed source into an application image.',
    test:'Check the application before downstream work can continue.',
    push:'Publish the image after all required checks succeed.',
    approval:'Pause for a person to review and approve the next step.',
    deploy:'Release the selected image to an existing destination.',
    custom:'A custom task configured by the pipeline author.'
  };
  const clone = value => JSON.parse(JSON.stringify(value));
  function initial(id) {
    const example=workspaceExamples.get(id);if(example)return clone({nodes:example.nodes,edges:example.edges});
    if(id==='n8n')return {nodes:[
      {id:'postgres',name:'PostgreSQL',kind:'deploy',description:'Start the database with its persistent volume. Wait for pg_isready before starting n8n.'},
      {id:'redis',name:'Redis queue',kind:'deploy',description:'Start Redis with append-only persistence and noeviction. Wait for its health check.'},
      {id:'main',name:'n8n main / web',kind:'deploy',description:'Start n8n in queue mode after PostgreSQL and Redis are healthy. Main completes database migrations before the worker starts.'},
      {id:'worker',name:'n8n worker',kind:'deploy',description:'Run the same image as main with worker --concurrency=5. Share the database, Redis connection, and encryption key. Wait for worker readiness.'}
    ],edges:[['postgres','main'],['redis','main'],['postgres','worker'],['redis','worker'],['main','worker']]};
    const nodes = id === 'storefront' ? [
      {id:'build',name:'Build image',kind:'build'},
      {id:'checkout',name:'Checkout tests',kind:'test'},
      {id:'api',name:'API tests',kind:'test'},
      {id:'push',name:'Push image',kind:'push'},
      {id:'review',name:'Review release',kind:'approval'},
      {id:'deploy',name:'Deploy staging',kind:'deploy'}
    ] : [
      {id:'build',name:'Build image',kind:'build'},
      {id:'test',name:'API tests',kind:'test'},
      {id:'push',name:'Push image',kind:'push'},
      {id:'deploy',name:'Deploy locally',kind:'deploy'}
    ];
    const edges = id === 'storefront'
      ? [['build','checkout'],['build','api'],['checkout','push'],['api','push'],['push','review'],['review','deploy']]
      : [['build','test'],['test','push'],['push','deploy']];
    return {nodes,edges};
  }
  const states = new Map();
  const names = {storefront:'Storefront release','content-api':'Content API release',n8n:'n8n queue mode',...Object.fromEntries(Object.entries(workspaceExamples.catalog).map(([id,d])=>[id,d.name]))};
  const supported = id => Object.hasOwn(names,id);
  function state(id) {
    if (!states.has(id)) {
      const graph=initial(id);
      states.set(id,{graph,selected:id==='storefront'?'checkout':id==='n8n'?'worker':workspaceExamples.get(id)?'build-web':'build',connection:null,history:[],saved:JSON.stringify(graph),savedOnce:false,zoom:null,view:'graph',counter:0});
    }
    return states.get(id);
  }
  function validate(graph) {
    const errors=[], ids=new Set(graph.nodes.map(n=>n.id));
    if (!graph.nodes.length) errors.push('Add at least one stage.');
    if (ids.size!==graph.nodes.length) errors.push('Stage IDs must be unique.');
    const adjacency=new Map([...ids].map(id=>[id,[]])), degree=new Map([...ids].map(id=>[id,0])), seen=new Set();
    for (const [source,target] of graph.edges) {
      if (!ids.has(source)||!ids.has(target)) {errors.push('Every connection needs two existing stages.');continue;}
      if (source===target) errors.push('A stage cannot depend on itself.');
      const key=source+'>'+target;
      if(seen.has(key)) {errors.push('This connection already exists.');continue;}
      seen.add(key);adjacency.get(source).push(target);degree.set(target,degree.get(target)+1);
    }
    const queue=[...ids].filter(id=>degree.get(id)===0);let visited=0;
    while(queue.length){const id=queue.shift();visited++;for(const next of adjacency.get(id)){degree.set(next,degree.get(next)-1);if(!degree.get(next))queue.push(next);}}
    if(visited!==ids.size) errors.push('That connection would create a loop. A DAG must move forward.');
    return [...new Set(errors)];
  }
  const nodeWidth=168, nodeHeight=120;
  const edgeKey = (from,to) => from+'>'+to;
  const serviceReady = (id,node) => Boolean(node.ready)||(id==='n8n'&&['postgres','redis','main','worker'].includes(node.id));
  function connectionInfo(id,graph,index) {
    const [from,to]=graph.edges[index],source=graph.nodes.find(n=>n.id===from),target=graph.nodes.find(n=>n.id===to);
    const healthy=serviceReady(id,source),approved=source.kind==='approval';
    const reasons={
      'postgres>main':'n8n needs its database to read and save workflows.',
      'redis>main':'n8n needs Redis to place workflow jobs in the queue.',
      'postgres>worker':'The worker reads workflows and saves execution results in PostgreSQL.',
      'redis>worker':'The worker takes waiting jobs from the Redis queue.',
      'main>worker':'Main finishes the initial database setup before the worker starts.'
    };
    return {from,to,source,target,healthy,approved,key:edgeKey(from,to),number:index+1,
      rule:`${target.name} waits until ${source.name} ${healthy?'is healthy':approved?'is approved':'finishes successfully'}.`,
      reason:workspaceExamples.get(id)?.reasons[edgeKey(from,to)]||(workspaceExamples.get(id)&&target.kind==='test'&&healthy?`The ${target.name.toLowerCase()} needs ${source.name.toLowerCase()} to be ready before it can check the deployed system.`:null)||id==='n8n'&&reasons[edgeKey(from,to)]||'This requirement keeps the next step from starting before its prerequisite is complete.'};
  }
  function layout(graph,narrow) {
    const rank=new Map(graph.nodes.map(n=>[n.id,0]));
    for(let i=0;i<graph.nodes.length;i++) for(const [from,to] of graph.edges) rank.set(to,Math.max(rank.get(to),rank.get(from)+1));
    const columns=[];
    for(const n of graph.nodes){const r=rank.get(n.id);(columns[r]||= []).push(n);}
    const max=Math.max(1,...columns.map(col=>col.length));
    const positions={};
    columns.forEach((col,r)=>col.forEach((n,i)=>{
      positions[n.id]=narrow ? {x:(max-col.length)*104+i*208,y:46+r*224} : {x:32+r*256,y:(max-col.length)*100+i*200};
    }));
    const skipped=graph.edges.filter(([from,to])=>rank.get(to)-rank.get(from)>1);
    const before=skipped.filter(([from,to])=>narrow?positions[from].x<=positions[to].x:positions[from].y<=positions[to].y).length, after=skipped.length-before;
    const start=narrow?32+Math.max(0,before-1)*44:before?72+(before-1)*44:46;
    const end=narrow?32+Math.max(0,after-1)*44:after?72+(after-1)*44:46;
    for(const p of Object.values(positions)){if(narrow)p.x+=start;else p.y+=start;}
    return {positions,rank,columns,width:narrow?start+end+max*208-40:64+columns.length*256-88,height:narrow?92+columns.length*224-104:start+end+max*200-80,narrow};
  }
  function connections(graph,drawing) {
    const {positions,narrow,rank}=drawing;
    const result=graph.edges.map(([from,to],index)=>{
      const a=positions[from],b=positions[to],skip=rank.get(to)-rank.get(from)>1;
      return {from,to,index,key:edgeKey(from,to),a,b,skip,side:(narrow?a.x<=b.x:a.y<=b.y)?-1:1};
    });
    const cross=p=>narrow?p.x:p.y;
    for(const edge of result){
      const outgoing=result.filter(e=>e.from===edge.from).sort((a,b)=>(a.skip?a.side*100000:cross(a.b))-(b.skip?b.side*100000:cross(b.b))||a.index-b.index);
      const incoming=result.filter(e=>e.to===edge.to).sort((a,b)=>(a.skip?a.side*100000:cross(a.a))-(b.skip?b.side*100000:cross(b.a))||a.index-b.index);
      const length=narrow?nodeWidth:nodeHeight;
      edge.out=(outgoing.indexOf(edge)+1)*length/(outgoing.length+1);
      edge.in=(incoming.indexOf(edge)+1)*length/(incoming.length+1);
      const lane=result.filter(e=>e.skip&&e.side===edge.side).indexOf(edge);
      if(narrow){
        const x1=edge.a.x+edge.out,y1=edge.a.y+nodeHeight,x2=edge.b.x+edge.in,y2=edge.b.y-7,mid=(y1+y2)/2;
        const bypass=edge.side<0?22+lane*44:drawing.width-22-lane*44;
        edge.d=edge.skip?`M${x1},${y1} V${y1+24+lane*10} H${bypass} V${y2-24-lane*10} H${x2} V${y2}`:`M${x1},${y1} C${x1},${mid} ${x2},${mid} ${x2},${y2}`;
        edge.label={x:edge.skip?bypass:(x1+x2)/2,y:mid};
      }else{
        const x1=edge.a.x+nodeWidth,y1=edge.a.y+edge.out,x2=edge.b.x-7,y2=edge.b.y+edge.in,mid=(x1+x2)/2;
        const bypass=edge.side<0?28+lane*44:drawing.height-28-lane*44;
        edge.d=edge.skip?`M${x1},${y1} H${x1+24+lane*10} V${bypass} H${x2-24-lane*10} V${y2} H${x2}`:`M${x1},${y1} C${mid},${y1} ${mid},${y2} ${x2},${y2}`;
        edge.label={x:mid,y:edge.skip?bypass:(y1+y2)/2};
      }
    }
    return result;
  }
  // Keep each numbered target on its own line, with room to select it at any zoom.
  // Dense forks can share a midpoint even though their paths/ports are distinct.
  function placeConnectionLabels(surface,drawing,scale) {
    const radius=22/scale, gap=6/scale, placed=[];
    const nodes=Object.values(drawing.positions).map(p=>({left:p.x,top:p.y,right:p.x+nodeWidth,bottom:p.y+nodeHeight}));
    const intersects=(a,b)=>a.left<b.right+gap&&a.right>b.left-gap&&a.top<b.bottom+gap&&a.bottom>b.top-gap;
    for(const path of surface.querySelectorAll('.dag-edges>path')){
      const button=[...surface.querySelectorAll('.dag-connection')].find(b=>b.dataset.dagEdge===path.dataset.edgeLine);
      if(!button)continue;
      const length=path.getTotalLength(),fractions=[.5];
      for(let i=1;i<=24;i++)fractions.push(.5-i*.02,.5+i*.02);
      for(const fraction of fractions){
        const p=path.getPointAtLength(length*fraction),rect={left:p.x-radius,top:p.y-radius,right:p.x+radius,bottom:p.y+radius};
        if(rect.left<0||rect.top<0||rect.right>drawing.width||rect.bottom>drawing.height||[...nodes,...placed].some(r=>intersects(rect,r)))continue;
        button.style.left=p.x+'px';button.style.top=p.y+'px';placed.push(rect);break;
      }
    }
  }
  const dirty = s => JSON.stringify(s.graph)!==s.saved;
  function remember(s){s.history.push(clone(s.graph));if(s.history.length>30)s.history.shift();}
  function summary(id){const s=state(id);return `${s.graph.nodes.length} stages · ${s.graph.edges.length} connections`;}
  function teaser(id){return `<a class="dag-entry" href="#pipeline/${id}"><span class="dag-entry-icon">${icon('flow')}</span><span><strong>Open the pipeline DAG</strong><small>${summary(id)} · ${id==='n8n'?'Published images · healthy service dependencies':id==='storefront'?'Parallel tests, then review and deploy':'Build, test, push, and deploy'}</small></span><span aria-hidden="true">↗</span></a>`;}
  function page(id) {
    const example=workspaceExamples.get(id);
    return pageHeading(names[id],example?example.description:id==='n8n'?'See what starts first, and what each service waits for.':'See what runs first, and what each step waits for.',`<a class="button" href="${example?'#pipelines':id==='n8n'?'#example/n8n':'#app/'+id+'/pipeline'}">${example?'All pipeline examples':id==='n8n'?'Back to n8n stack':'Back to App'}</a>`,'PIPELINE / DAG')+`
    ${workspaceExamples.chooser(id)}
    <div class="dag-workspace ${example?'dag-large-example':''}" data-dag-id="${id}">
      <div class="dag-reading-guide"><span class="dag-guide-symbol" aria-hidden="true">A → B</span><div><strong>${example?'Every arrow explains what the next step waits for.':id==='n8n'?'A must be healthy before B can start.':'A must finish successfully before B can start.'}</strong><p>${example?'Tasks must succeed; services must become healthy. This is an illustrative release plan. ':id==='n8n'?'This is the startup order. ':'Follow each arrow toward the step that waits. '}Select a numbered connection to see why it is needed.</p></div>${id==='n8n'?'<a href="#example/n8n">How the services work together ↗</a>':''}</div>
      ${example?`<div class="dag-phase-guide"><p><strong>${example.size} deployment · ${example.nodes.length} steps</strong><span>Jump through the release to explore the full map.</span></p><div aria-label="Release phases">${example.phases.map((phase,i)=>`<button type="button" data-dag-phase="${phase}"><span>${i+1}</span>${phase}</button>`).join('')}</div><label for="dag-jump">Find a step</label><select id="dag-jump"></select><small>Scroll inside the map to pan. Selecting a phase or step brings it into view at a readable size. Step list shows every requirement as text.</small></div>`:''}
      <div class="dag-commandbar"><div><span class="dag-draft-label">EXAMPLE DRAFT</span><strong id="dag-counts"></strong><span id="dag-save-state" role="status"></span></div><div class="dag-actions"><button class="button" type="button" data-dag-action="add">${icon('plus')} Add stage</button><button class="button" type="button" data-dag-action="validate">Validate</button><button class="button primary" type="button" data-dag-action="save">Save example</button></div></div>
      <div class="dag-layout"><section class="dag-board" aria-label="Pipeline graph editor">
        <div class="dag-toolbar"><div class="dag-view-switch" aria-label="Pipeline view"><button type="button" data-dag-view="graph" aria-pressed="true">DAG</button><button type="button" data-dag-view="steps" aria-pressed="false">Step list</button></div><div class="dag-zoom-controls"><button type="button" data-dag-action="out" aria-label="Zoom out">−</button><output id="dag-zoom" aria-label="Graph zoom"></output><button type="button" data-dag-action="in" aria-label="Zoom in">+</button><button type="button" data-dag-action="fit">${example?'Reset view':'Fit'}</button></div></div>
        <div id="dag-viewport" class="dag-viewport" role="region" tabindex="0" aria-label="Pipeline DAG. Select a stage; scroll to pan." aria-describedby="dag-help"><div id="dag-space"><div id="dag-surface"></div></div></div>
        <div id="dag-step-list" class="dag-step-list" hidden></div>
        <div class="dag-legend"><span><i aria-hidden="true"></i> Arrow points to the step that waits</span><span>Each number is one requirement</span></div>
        <div class="dag-connection-summary" id="dag-connection-summary" aria-live="polite"></div>
        <details class="dag-connection-directory"><summary>All connections, in plain language <span id="dag-connection-count"></span></summary><ol id="dag-connections"></ol></details>
        <div class="dag-board-footer"><span id="dag-help">Select a step or numbered arrow · scroll to pan</span><button type="button" class="button" data-dag-action="undo" disabled>Undo edit</button></div>
      </section><aside id="dag-inspector" class="panel dag-inspector" aria-label="Selected DAG stage"></aside></div>
      <p id="dag-feedback" class="dag-feedback" role="status">This is an editable example. Nothing runs or deploys from this canvas.</p>
      <details class="dag-explainer"><summary>Technical detail: how this DAG works</summary><p>A directed acyclic graph shows which steps depend on others. A fork allows parallel work; a join waits for every incoming dependency to succeed. Loops are not allowed. Separate arrowheads enter separate ports; crossing lines do not add a dependency.</p><p>${example?'This sample has not been executed or imported into Cooker. Its service-readiness and approval gates describe the intended plan; actual adapter support must be checked before backend execution. Production infrastructure and compatible schema are assumptions, not provisioned resources.':id==='n8n'?'This initial graph matches the four-service Compose example’s dependency order. Every service uses a published image, so there are no build or push stages. Native Compose preserves service_healthy conditions. Editing here does not change the Compose file or repeat the completed local test.':'This authored pipeline is separate from the App’s Compose review. Existing recorded runs show an earlier four-stage example.'} Saving here only keeps the draft for this visit.</p><a href="docs.html#runs">Read the pipeline guide ↗</a></details>
      <dialog id="dag-add-dialog" aria-labelledby="dag-add-title"><div class="dialog-heading"><div><p class="eyebrow">GROW THE PIPELINE</p><h2 id="dag-add-title">Add a stage</h2></div><button class="icon-button" type="button" data-dag-action="close-add" aria-label="Close add stage">×</button></div><form id="dag-add-form"><label class="field-label" for="dag-new-kind">Kind of work</label><select id="dag-new-kind">${Object.entries(kinds).map(([key,value])=>`<option value="${key}">${value}</option>`).join('')}</select><label class="field-label" for="dag-new-name">Stage name</label><input id="dag-new-name" maxlength="48" required placeholder="For example, Smoke tests"><label class="field-label" for="dag-new-after">Run after</label><select id="dag-new-after"></select><p class="field-hint">Add a branch or extend the flow. You can edit connections in the inspector.</p><p id="dag-add-error" class="field-error" role="status"></p><button type="submit" class="button primary">Add to example</button></form></dialog>
    </div>`;
  }
  let observer;
  function unmount(){observer?.disconnect();observer=null;}
  function mount(id){
    unmount();const root=document.querySelector('[data-dag-id]');if(!root)return;
    const s=state(id), q=selector=>root.querySelector(selector), viewport=q('#dag-viewport');
    let drawing, scale=1, lastWidth=0;
    const say=(message,error=false)=>{q('#dag-feedback').textContent=message;q('#dag-feedback').classList.toggle('is-error',error);};
    const selected=()=>s.graph.nodes.find(n=>n.id===s.selected)||s.graph.nodes[0];
    function paint(){
      drawing=layout(s.graph,window.innerWidth<=780);
      root.dataset.direction=drawing.narrow?'vertical':'horizontal';
      const width=viewport.clientWidth||root.clientWidth;
      const fit=Math.min((width-24)/drawing.width,((viewport.clientHeight||430)-24)/drawing.height);
      scale=s.zoom??Math.min(1,Math.max(.5,fit));
      const surface=q('#dag-surface');surface.style.width=drawing.width+'px';surface.style.height=drawing.height+'px';surface.style.transform=`scale(${scale})`;
      surface.style.setProperty('--dag-scale',scale);
      q('#dag-space').style.width=Math.max(width,drawing.width*scale+24)+'px';q('#dag-space').style.height=drawing.height*scale+24+'px';
      const drawn=connections(s.graph,drawing), active=e=>s.connection?e.key===s.connection:e.from===s.selected||e.to===s.selected;
      const chosen=drawn.find(e=>e.key===s.connection);
      const paths=drawn.map(e=>`<path data-edge-line="${esc(e.key)}" d="${e.d}" class="${active(e)?'selected':''}" marker-end="url(#dag-arrow${active(e)?'-active':''})"/>`).join('');
      const hitPaths=drawn.map(e=>`<path data-dag-edge="${esc(e.key)}" d="${e.d}"/>`).join('');
      const markers=['','-active'].map(suffix=>`<marker id="dag-arrow${suffix}" viewBox="0 0 12 12" refX="11" refY="6" markerWidth="12" markerHeight="12" markerUnits="userSpaceOnUse" orient="auto"><path d="M1 1 11 6 1 11 4 6z"/></marker>`).join('');
      const badges=drawn.map(e=>{const info=connectionInfo(id,s.graph,e.index);return `<button type="button" class="dag-connection ${active(e)?'is-related':''}" data-dag-edge="${esc(e.key)}" aria-pressed="${e.key===s.connection}" aria-label="Connection ${info.number}: ${esc(info.rule)}" style="left:${e.label.x}px;top:${e.label.y}px"><span>${info.number}</span></button>`;}).join('');
      surface.innerHTML=`<svg class="dag-edges ${s.connection?'has-connection':''}" width="${drawing.width}" height="${drawing.height}" aria-hidden="true"><defs>${markers}</defs>${paths}<g class="dag-edge-hits">${hitPaths}</g></svg>`+s.graph.nodes.map(n=>{
        const p=drawing.positions[n.id],incoming=s.graph.edges.filter(e=>e[1]===n.id).length;
        const ports=drawn.flatMap(e=>[...(e.to===n.id?[`<span class="dag-port in" data-port-edge="${esc(e.key)}" style="--port:${e.in}px" aria-hidden="true"></span>`]:[]),...(e.from===n.id?[`<span class="dag-port out" data-port-edge="${esc(e.key)}" style="--port:${e.out}px" aria-hidden="true"></span>`]:[])]).join('');
        return `<button type="button" class="dag-node ${chosen&&(chosen.from===n.id||chosen.to===n.id)?'is-endpoint':''}" data-dag-node="${n.id}" data-kind="${n.kind}" style="left:${p.x}px;top:${p.y}px" aria-pressed="${n.id===s.selected&&!s.connection}" aria-label="${esc(n.name)}, ${kinds[n.kind]} stage"><span class="dag-node-top">${icon(symbols[n.kind]||n.kind)}<span>${id==='n8n'&&serviceReady(id,n)?'Start service':kinds[n.kind]}</span><span class="dag-node-dot" aria-hidden="true"></span></span><strong>${esc(n.name)}</strong><small>${incoming?`Waits for ${incoming>1?'all ':''}${incoming} ${incoming===1?'step':'steps'}`:'Can start immediately'}</small>${ports}</button>`;
      }).join('')+badges;
      if(s.graph.nodes.length>8)placeConnectionLabels(surface,drawing,scale);
      if(q('#dag-jump'))q('#dag-jump').innerHTML=s.graph.nodes.map(n=>`<option value="${n.id}" ${n.id===s.selected?'selected':''}>${esc(n.phase||'Added step')} · ${esc(n.name)}</option>`).join('');
      root.querySelectorAll('[data-dag-phase]').forEach(b=>b.setAttribute('aria-pressed',String(selected().phase===b.dataset.dagPhase)));
      q('#dag-counts').textContent=summary(id);q('#dag-save-state').textContent=dirty(s)?'Unsaved edits':s.savedOnce?'Saved for this visit':'Ready to explore';
      q('#dag-zoom').textContent=Math.round(scale*100)+'%';
      q('[data-dag-action="out"]').disabled=scale<=.5;q('[data-dag-action="in"]').disabled=scale>=1.5;q('[data-dag-action="undo"]').disabled=!s.history.length;
      q('#dag-step-list').innerHTML=s.graph.nodes.map(n=>{const inputs=s.graph.edges.filter(e=>e[1]===n.id);return `<button type="button" class="dag-list-node" data-dag-node="${n.id}" aria-pressed="${n.id===s.selected&&!s.connection}"><span>${icon(symbols[n.kind]||n.kind)}</span><span><strong>${esc(n.name)}</strong><small>${inputs.length?'Waits for '+(inputs.length>1?'all of: ':'')+esc(inputs.map(e=>s.graph.nodes.find(n=>n.id===e[0]).name).join(', ')):'Can start immediately — no earlier step required.'}</small></span><span aria-hidden="true">→</span></button>`;}).join('');
      q('#dag-connection-count').textContent=`(${drawn.length})`;
      q('#dag-connections').innerHTML=drawn.map(e=>{const info=connectionInfo(id,s.graph,e.index);return `<li><button type="button" data-dag-edge="${esc(e.key)}" aria-pressed="${e.key===s.connection}"><span class="dag-connection-number">${info.number}</span><span><strong>${esc(info.source.name)} <span aria-hidden="true">→</span> ${esc(info.target.name)}</strong><small>${esc(info.rule)}</small></span></button></li>`;}).join('');
      const n=selected(),inputs=s.graph.edges.filter(e=>e[1]===n.id);
      q('#dag-connection-summary').innerHTML=chosen?`<strong>Connection ${chosen.index+1}: ${esc(connectionInfo(id,s.graph,chosen.index).rule)}</strong><p>${esc(connectionInfo(id,s.graph,chosen.index).reason)}</p>`:`<strong>${esc(n.name)}${inputs.length?` waits for ${inputs.length>1?'all ':''}${inputs.length} ${inputs.length===1?'step':'steps'}`:' can start immediately'}</strong><p>${inputs.length?esc(inputs.map(e=>s.graph.nodes.find(n=>n.id===e[0]).name).join(' + '))+'. Each has its own arrow.':'No incoming arrows means no earlier step is required.'}</p>`;
      viewport.hidden=s.view!=='graph';q('#dag-step-list').hidden=s.view!=='steps';q('.dag-zoom-controls').hidden=s.view!=='graph';
      root.querySelectorAll('[data-dag-view]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.dagView===s.view)));
    }
    function inspect(){
      const edgeIndex=s.graph.edges.findIndex(([from,to])=>edgeKey(from,to)===s.connection);
      if(edgeIndex!==-1){
        const info=connectionInfo(id,s.graph,edgeIndex);
        q('#dag-inspector').innerHTML=`<div class="dag-inspector-heading"><span class="dag-connection-number">${info.number}</span><div><p class="eyebrow">SELECTED CONNECTION</p><h2>Why this arrow?</h2></div></div><p class="dag-rule">${esc(info.rule)}</p><ol class="dag-connection-story"><li><span>FIRST</span><button type="button" data-dag-node="${info.from}">${esc(info.source.name)}</button><small>${info.healthy?'Becomes healthy and ready.':info.approved?'Receives the required approval.':'Finishes successfully.'}</small></li><li><span>THEN</span><button type="button" data-dag-node="${info.to}">${esc(info.target.name)}</button><small>Can start once all its requirements are met.</small></li></ol><div class="dag-why"><strong>Why it matters</strong><p>${esc(info.reason)}</p></div><p class="dag-description">This arrow is one requirement. The next step may have other incoming arrows, too.</p><button type="button" class="button dag-show-requirements" data-dag-node="${info.to}">See all requirements</button><details class="dag-connection-technical"><summary>Technical detail</summary><p>${info.healthy&&id!=='n8n'?'This example requires a service-readiness gate. It is illustrative and has not been run through a backend adapter.':info.healthy?'The original n8n Compose example uses <code>depends_on</code> with <code>condition: service_healthy</code>.':'This draft uses a success dependency between stages.'} This is an editable design example. It does not change a Compose file or execute work.</p></details>`;
        return;
      }
      const n=selected();s.selected=n.id;
      const inputs=s.graph.edges.filter(e=>e[1]===n.id),outputs=s.graph.edges.filter(e=>e[0]===n.id);
      const plain={postgres:'Stores your workflows and their results.',redis:'Keeps the list of jobs waiting to run.',main:'Opens the editor and receives triggers and webhooks.',worker:'Takes queued jobs and runs your workflows.'};
      const requirements=inputs.map(([from,to])=>{const index=s.graph.edges.findIndex(e=>e[0]===from&&e[1]===to),info=connectionInfo(id,s.graph,index);return `<li><button type="button" data-dag-edge="${esc(info.key)}"><span class="dag-connection-number">${info.number}</span><span><strong>${esc(info.source.name)}</strong><small>${info.healthy?'Must be healthy':info.approved?'Must be approved':'Must finish successfully'}</small></span><span aria-hidden="true">↗</span></button></li>`;}).join('');
      q('#dag-inspector').innerHTML=`<div class="dag-inspector-heading"><span>${icon(symbols[n.kind]||n.kind)}</span><div><p class="eyebrow">SELECTED STEP</p><h2>${esc(n.name)}</h2></div></div><p class="dag-description">${esc(id==='n8n'&&plain[n.id]||n.description||explanations[n.kind])}</p><h3 class="dag-section-label">${inputs.length?'Before this can start':'Ready to start'}</h3><p class="dag-prerequisite-intro">${inputs.length?`All ${inputs.length} ${inputs.length===1?'requirement must':'requirements must'} be met. Select one to see why.`:'No earlier step is required. It can start alongside other starting steps.'}</p><ol class="dag-prerequisites">${requirements}</ol><h3 class="dag-section-label">What can happen next?</h3><p class="dag-outgoing">${outputs.length?outputs.map(e=>esc(s.graph.nodes.find(n=>n.id===e[1]).name)).join(' and ')+' can continue once their other requirements are also met.':'This is the last step in this branch.'}</p><details class="dag-edit-settings" ${s.editing?'open':''}><summary>Edit step and connections</summary><span class="badge">${kinds[n.kind]}</span><p class="dag-technical-description">${esc(n.description||explanations[n.kind])}</p><form id="dag-name-form"><label class="field-label" for="dag-stage-name">Stage name</label><input id="dag-stage-name" required maxlength="48" value="${esc(n.name)}"><button type="submit" class="button dag-rename">Apply name</button></form><h3 class="dag-section-label">Runs after</h3><p class="field-hint">Every listed stage must succeed first.</p><ul class="dag-dependencies">${inputs.length?inputs.map(([source])=>`<li><span>${esc(s.graph.nodes.find(n=>n.id===source).name)}</span><button type="button" data-remove-dependency="${source}" aria-label="Remove dependency on ${esc(s.graph.nodes.find(n=>n.id===source).name)}">×</button></li>`).join(''):'<li class="muted">No dependencies. Starts the flow.</li>'}</ul><form id="dag-connect-form"><label class="sr-only" for="dag-dependency">Add a dependency</label><div class="dag-connect-row"><select id="dag-dependency"><option value="">Choose a stage…</option>${s.graph.nodes.filter(other=>other.id!==n.id&&!inputs.some(e=>e[0]===other.id)).map(other=>`<option value="${other.id}">${esc(other.name)}</option>`).join('')}</select><button type="submit" class="button">Connect</button></div><p id="dag-connect-error" class="field-error" role="status"></p></form><p class="dag-inspector-hint">${inputs.length>1?'This join waits for all incoming stages.':outputs.length>1?'This fork allows the next stages to run in parallel.':'Connections move work forward, without loops.'}</p><button type="button" class="button dag-remove" data-dag-action="remove" ${s.graph.nodes.length===1?'disabled':''}>Remove stage</button></details>`;
    }
    function refresh(focusNode=false){s.editing=q('.dag-edit-settings')?.open??s.editing;paint();inspect();if(focusNode){const container=s.view==='graph'?q('#dag-surface'):q('#dag-step-list');container.querySelector(`[data-dag-node="${s.selected}"]`)?.focus({preventScroll:true});}}
    function changed(message){s.zoom=null;s.connection=null;refresh();say(message);}
    function jumpTo(nodeId){
      if(!s.graph.nodes.some(n=>n.id===nodeId))return;
      s.selected=nodeId;s.connection=null;s.view='graph';s.zoom=1;refresh();
      const p=drawing.positions[nodeId];viewport.scrollTo({left:Math.max(0,12+(p.x+nodeWidth/2)*scale-viewport.clientWidth/2),top:Math.max(0,12+(p.y+nodeHeight/2)*scale-viewport.clientHeight/2),behavior:'instant'});
    }
    root.addEventListener('change',event=>{if(event.target.id==='dag-jump')jumpTo(event.target.value);});
    root.addEventListener('click',event=>{
      const phase=event.target.closest('[data-dag-phase]');if(phase){const n=s.graph.nodes.find(n=>n.phase===phase.dataset.dagPhase);if(n)jumpTo(n.id);return;}

      const node=event.target.closest('[data-dag-node]');if(node){s.selected=node.dataset.dagNode;s.connection=null;refresh(true);return;}
      const connection=event.target.closest('[data-dag-edge]');if(connection){
        s.connection=connection.dataset.dagEdge;
        const edge=s.graph.edges.find(([from,to])=>edgeKey(from,to)===s.connection);if(!edge)return;
        s.selected=edge[1];
        const inDirectory=Boolean(connection.closest('#dag-connections'));
        refresh();
        const container=inDirectory?q('#dag-connections'):q('#dag-surface');
        if(s.view==='graph'||inDirectory)[...container.querySelectorAll('button[data-dag-edge]')].find(b=>b.dataset.dagEdge===s.connection)?.focus({preventScroll:true});
        else {const heading=q('#dag-inspector h2');heading.tabIndex=-1;heading.focus({preventScroll:true});}
        return;
      }
      const view=event.target.closest('[data-dag-view]');if(view){s.view=view.dataset.dagView;paint();return;}
      const remove=event.target.closest('[data-remove-dependency]');if(remove){remember(s);s.graph.edges=s.graph.edges.filter(e=>!(e[0]===remove.dataset.removeDependency&&e[1]===s.selected));changed('Connection removed. Undo restores it.');q('#dag-dependency').focus();return;}
      const action=event.target.closest('[data-dag-action]')?.dataset.dagAction;
      if(action==='in'||action==='out'){s.zoom=Math.min(1.5,Math.max(.5,Math.round((scale+(action==='in'?.1:-.1))*100)/100));paint();}
      if(action==='fit'){s.zoom=null;paint();viewport.scrollTo(0,0);}
      if(action==='validate'){const errors=validate(s.graph);say(errors.length?errors.join(' '):`DAG is valid: ${summary(id)}, no loops. This checks graph structure only.`,Boolean(errors.length));}
      if(action==='save'){const errors=validate(s.graph);if(errors.length){say(errors.join(' '),true);return;}s.saved=JSON.stringify(s.graph);s.savedOnce=true;paint();say('Example saved for this visit. Refreshing clears it; no pipeline was sent or executed.');}
      if(action==='undo'&&s.history.length){s.graph=s.history.pop();if(!s.graph.nodes.some(n=>n.id===s.selected))s.selected=s.graph.nodes[0].id;changed('Previous graph restored.');}
      if(action==='remove'&&s.graph.nodes.length>1){const n=selected();remember(s);s.graph.nodes=s.graph.nodes.filter(node=>node.id!==n.id);s.graph.edges=s.graph.edges.filter(e=>!e.includes(n.id));s.selected=s.graph.nodes[0].id;changed('Stage and its connections removed. Undo restores them.');q('[data-dag-action="undo"]').focus();}
      if(action==='add'){q('#dag-add-form').reset();q('#dag-add-error').textContent='';q('#dag-new-after').innerHTML='<option value="">No dependency · new starting point</option>'+s.graph.nodes.map(n=>`<option value="${n.id}" ${n.id===s.selected?'selected':''}>${esc(n.name)}</option>`).join('');q('#dag-add-dialog').showModal();q('#dag-new-name').focus();}
      if(action==='close-add')q('#dag-add-dialog').close();
    });
    root.addEventListener('submit',event=>{
      event.preventDefault();
      if(event.target.id==='dag-name-form'){
        const input=q('#dag-stage-name'),name=input.value.trim();if(!name){input.setCustomValidity('Enter a stage name.');input.reportValidity();return;}remember(s);selected().name=name;refresh();say('Stage name updated in this example.');q('#dag-stage-name').focus();
      }
      if(event.target.id==='dag-connect-form'){
        const from=q('#dag-dependency').value;if(!from){q('#dag-connect-error').textContent='Choose a stage to connect.';q('#dag-dependency').focus();return;}
        const next=clone(s.graph);next.edges.push([from,s.selected]);const errors=validate(next);
        if(errors.length){q('#dag-connect-error').textContent=errors.join(' ');q('#dag-dependency').focus();return;}
        remember(s);s.graph=next;changed('Dependency added. The selected stage waits for it to succeed.');q('#dag-dependency').focus();
      }
      if(event.target.id==='dag-add-form'){
        const name=q('#dag-new-name').value.trim();if(!name){q('#dag-add-error').textContent='Enter a stage name.';return;}
        const kind=q('#dag-new-kind').value,from=q('#dag-new-after').value,nodeId='stage-'+(++s.counter);remember(s);
        s.graph.nodes.push({id:nodeId,name,kind});if(from)s.graph.edges.push([from,nodeId]);s.selected=nodeId;q('#dag-add-dialog').close();changed('Stage added. Select it to edit its dependencies.');
      }
    });
    root.addEventListener('input',event=>{if(event.target.id==='dag-stage-name')event.target.setCustomValidity('');});
    q('#dag-add-dialog').addEventListener('keydown',event=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();q('#dag-add-dialog').close();}});
    refresh();
    observer=new ResizeObserver(()=>{const width=viewport.clientWidth;if(width&&width!==lastWidth){lastWidth=width;paint();}});observer.observe(viewport);
  }
  return {supported,summary,teaser,page,mount,unmount};
})();
