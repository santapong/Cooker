// Optional OGL light; readable content and workflow actions remain native HTML.
const scene = document.querySelector('#kitchen-scene');
const image = document.querySelector('#kitchen-image');
const canvas = document.querySelector('#kitchen-light');
const button = document.querySelector('#motion-toggle');
const label = document.querySelector('#motion-label');
const reduced = window.matchMedia('(prefers-reduced-motion: reduce)');
const pointsPerTrail = 32;
// Coordinates refer to the exact selected 1536×1024 image. Separate tracks
// keep the light from drawing over the wall that occludes the middle route.
const paths = [
  [[312,238],[407,285],[507,337],[568,376],[605,407],[622,431],[619,448],[606,466],[568,486]],
  [[312,238],[408,286],[507,337],[579,370],[638,383],[665,391],[687,419],[687,444],[679,473],[686,502],[726,534],[783,573],[859,618]],
  [[1179,565],[1207,579],[1227,599],[1235,621],[1227,647],[1212,678],[1207,706],[1217,731],[1241,751],[1295,773],[1345,781]],
];
function curve(points) {
  const values = [];
  for (let i=0;i<points.length-1;i++) {
    const a=points[Math.max(0,i-1)],b=points[i],c=points[i+1],d=points[Math.min(points.length-1,i+2)];
    for (let j=0;j<16;j++) {
      const t=j/16,t2=t*t,t3=t2*t;
      values.push([0,1].map(k=>.5*((2*b[k])+(-a[k]+c[k])*t+(2*a[k]-5*b[k]+4*c[k]-d[k])*t2+(-a[k]+3*b[k]-3*c[k]+d[k])*t3)));
    }
  }
  values.push(points.at(-1)); const lengths=[0];
  for (let i=1;i<values.length;i++) lengths.push(lengths.at(-1)+Math.hypot(values[i][0]-values[i-1][0],values[i][1]-values[i-1][1]));
  return {values,lengths,total:lengths.at(-1)};
}
const tracks=paths.map(curve);
function sample(track,fraction) {
  const distance=Math.max(0,Math.min(1,fraction))*track.total; let i=1;
  while(i<track.lengths.length-1 && track.lengths[i]<distance)i++;
  const t=(distance-track.lengths[i-1])/(track.lengths[i]-track.lengths[i-1]||1);
  return track.values[i-1].map((v,k)=>v+(track.values[i][k]-v)*t);
}
let renderer,geometry,program,mesh,gl,frame=0,last=0,elapsed=0,inView=false,paused=false,disposed=false,initializing=false;
const positions=new Float32Array(tracks.length*pointsPerTrail*2);
const strengths=new Float32Array(tracks.length*pointsPerTrail);
function stop(){if(frame)cancelAnimationFrame(frame);frame=0;last=0;}
function usable(){return !disposed&&!paused&&!reduced.matches&&inView&&!document.hidden;}
function mark(state){scene.dataset.motion=state;}
function resize(){
  if(!renderer)return;
  const rect=image.getBoundingClientRect(); renderer.dpr=Math.min(window.devicePixelRatio||1,1.5);
  renderer.setSize(Math.max(1,Math.round(rect.width)),Math.max(1,Math.round(rect.height)));
  program.uniforms.uDpr.value=renderer.dpr;
}
function draw(now){
  frame=0;if(!usable()){last=0;return;}
  if(last&&now-last<1000/30){frame=requestAnimationFrame(draw);return;}
  if(last)elapsed+=Math.min(now-last,100)/1000;last=now;
  tracks.forEach((track,k)=>{
    const head=((elapsed/9+k*.19)%1.25)-.05;
    for(let j=0;j<pointsPerTrail;j++){
      const t=head-j*.0034;const [x,y]=sample(track,t);const index=k*pointsPerTrail+j;
      positions[index*2]=x/1536*2-1;positions[index*2+1]=1-y/1024*2;
      strengths[index]=t>=0&&t<=1?Math.pow(1-j/pointsPerTrail,1.8):0;
    }
  });
  geometry.attributes.position.needsUpdate=true;geometry.attributes.strength.needsUpdate=true;
  renderer.render({scene:mesh});canvas.dataset.frames=String(Number(canvas.dataset.frames||0)+1);
  mark('running');frame=requestAnimationFrame(draw);
}
function sync(){
  stop();button.setAttribute('aria-pressed',String(paused));
  label.textContent=reduced.matches?'Still view':paused?'Resume light':'Pause light';canvas.hidden=reduced.matches;
  if(reduced.matches){mark('reduced');return;}
  if(!usable()){mark(paused?'paused':'idle');return;}
  if(renderer)frame=requestAnimationFrame(draw);else initialize();
}
function fallback(){
  if(disposed)return;
  disposed=true;stop();canvas.hidden=true;button.hidden=true;mark('static');
  geometry?.remove();program?.remove();gl?.getExtension('WEBGL_lose_context')?.loseContext();renderer=null;
}
async function initialize(){
  if(initializing||renderer||disposed||reduced.matches)return;initializing=true;
  try{
    const base=new URL('./vendor/ogl/src/',import.meta.url);
    const [{Renderer},{Geometry},{Program},{Mesh}]=await Promise.all([
      import(new URL('core/Renderer.js',base).href),import(new URL('core/Geometry.js',base).href),
      import(new URL('core/Program.js',base).href),import(new URL('core/Mesh.js',base).href),
    ]);
    if(disposed)return;
    renderer=new Renderer({canvas,alpha:true,depth:false,antialias:false,powerPreference:'low-power',webgl:1,premultipliedAlpha:false});
    gl=renderer.gl;if(!gl)throw new Error('WebGL unavailable');
    geometry=new Geometry(gl,{position:{size:2,data:positions},strength:{size:1,data:strengths}});
    program=new Program(gl,{
      vertex:`attribute vec2 position; attribute float strength; uniform float uDpr; varying float vStrength; void main(){vStrength=strength;gl_Position=vec4(position,0.0,1.0);gl_PointSize=(15.0+8.0*strength)*uDpr;}`,
      fragment:`precision mediump float; varying float vStrength; void main(){vec2 p=gl_PointCoord-0.5;float d=dot(p,p);float halo=exp(-d*25.0)*0.27;float core=exp(-d*260.0);float a=(halo+core)*vStrength;gl_FragColor=vec4(mix(vec3(1.0,0.36,0.055),vec3(1.0,0.91,0.66),core),a);}`,
      uniforms:{uDpr:{value:1}},transparent:true,depthTest:false,depthWrite:false,cullFace:null,
    });
    if(!gl.getProgramParameter(program.program,gl.LINK_STATUS))throw new Error('Shader unavailable');
    program.setBlendFunc(gl.SRC_ALPHA,gl.ONE);
    mesh=new Mesh(gl,{geometry,program,mode:gl.POINTS,frustumCulled:false});
    resize();button.hidden=false;mark('ready');sync();
  }catch{fallback();}finally{initializing=false;}
}
button.addEventListener('click',()=>{paused=!paused;sync();});reduced.addEventListener('change',sync);
document.addEventListener('visibilitychange',sync);
canvas.addEventListener('webglcontextlost',event=>{event.preventDefault();fallback();});
const resizeObserver=new ResizeObserver(resize);resizeObserver.observe(image);
const observer=new IntersectionObserver(entries=>{inView=entries[0].isIntersecting;sync();},{threshold:.05});observer.observe(image);
window.addEventListener('pagehide',event=>{stop();if(!event.persisted){disposed=true;observer.disconnect();resizeObserver.disconnect();geometry?.remove();program?.remove();gl?.getExtension('WEBGL_lose_context')?.loseContext();}});
window.addEventListener('pageshow',()=>{if(!disposed)sync();});mark(reduced.matches?'reduced':'idle');
