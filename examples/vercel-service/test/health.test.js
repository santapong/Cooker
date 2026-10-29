import test from 'node:test';
import assert from 'node:assert/strict';
import health from '../api/health.js';
test('health returns a noncached response and a structured log without request secrets',()=>{
  const logs=[];const original=console.info;console.info=value=>logs.push(JSON.parse(value));
  const headers={};let status,body;
  const response={setHeader:(k,v)=>headers[k]=v,status:(value)=>{status=value;return response;},json:value=>body=value};
  try{health({headers:{authorization:'TEST_ONLY_MUST_NOT_BE_LOGGED'}},response);}finally{console.info=original;}
  assert.equal(status,200);assert.equal(headers['Cache-Control'],'no-store');assert.equal(body.status,'ok');assert.equal(logs.length,1);assert.equal(logs[0].event,'health_check');assert.ok(!JSON.stringify(logs).includes('TEST_ONLY_MUST_NOT_BE_LOGGED'));assert.ok(!Number.isNaN(Date.parse(body.timestamp)));
});
