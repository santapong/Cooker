const button=document.querySelector('#check');
button.addEventListener('click',async()=>{
  button.disabled=true;
  try{const response=await fetch('/api/health',{cache:'no-store'});if(!response.ok)throw new Error('Health request failed');document.querySelector('#result').textContent=JSON.stringify(await response.json(),null,2);}
  catch{document.querySelector('#result').textContent='Health is unavailable. Try again.';}
  finally{button.disabled=false;}
});
