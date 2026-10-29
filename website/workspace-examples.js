'use strict';
// Illustrative authored pipelines for reviewing graph density, not executed stacks.
const workspaceExamples = (() => {
  const node = (id,name,kind,phase,description,ready=false) => ({id,name,kind,phase,description,ready});
  const medium = {
    name:'Web app deployment',size:'Medium',selected:'api',
    description:'Two images, a database and a cache. Build in parallel, prepare the data, then release the app.',
    phases:['Build','Check','Publish','Data','Release','Verify'],
    nodes:[
      node('build-web','Build web','build','Build','Package the customer-facing website from the reviewed source.'),
      node('build-api','Build API','build','Build','Package the application API from the same reviewed revision.'),
      node('test-web','Test web','test','Check','Check the website before publishing its image.'),
      node('test-api','Test API','test','Check','Check API behavior before publishing its image.'),
      node('push-web','Publish web','push','Publish','Publish the tested website image and record its exact digest.'),
      node('push-api','Publish API','push','Publish','Publish the tested API image and record its exact digest.'),
      node('database','Database ready','deploy','Data','Wait for the example database to pass its readiness check.',true),
      node('cache','Cache ready','deploy','Data','Wait for the example cache to be ready for requests.',true),
      node('migrate','Update schema','custom','Data','Run the reviewed database migration once before releasing the API.'),
      node('api','Release API','deploy','Release','Start the published API after the schema update and cache readiness check.',true),
      node('web','Release web','deploy','Release','Start the published website after its API is healthy.',true),
      node('smoke','Check the app','test','Verify','Check the website and API together before declaring this attempt successful.')
    ],
    edges:[['build-web','test-web'],['build-api','test-api'],['test-web','push-web'],['test-api','push-api'],['database','migrate'],['push-api','migrate'],['migrate','api'],['cache','api'],['push-web','web'],['api','web'],['api','smoke'],['web','smoke']],
    reasons:{'database>migrate':'The schema update needs a healthy database.','push-api>migrate':'Run the migration from the exact API image that passed its checks.','migrate>api':'The API needs the reviewed database schema before it starts.','cache>api':'The API uses the cache and waits for it to be ready.','push-web>web':'Release the tested website image, identified by its recorded digest.','api>web':'The website needs a healthy API to serve its users.','api>smoke':'Verify the API is serving requests before checking the complete app.','web>smoke':'The complete-app check needs the website to be reachable.'}
  };
  const complex = {
    name:'SaaS platform release',size:'Complex',selected:'approve',
    description:'Web, API and worker services. Parallel checks join at staging, a person reviews the result, then production is verified.',
    phases:['Build','Check','Publish','Staging','Review','Production','Verify'],
    nodes:[
      ...['web','api','worker'].map(s=>node('build-'+s,'Build '+(s==='api'?'API':s),'build','Build','Package the '+s+' service from the reviewed revision.')),
      ...['web','api','worker'].map(s=>node('test-'+s,'Test '+(s==='api'?'API':s),'test','Check','Run the '+s+' service checks. Failure blocks its image publication.')),
      ...['web','api','worker'].map(s=>node('push-'+s,'Publish '+(s==='api'?'API':s),'push','Publish','Publish the tested '+s+' image and record its exact digest.')),
      node('database','Staging database','deploy','Staging','Wait for the staging database to become ready.',true),
      node('queue','Staging queue','deploy','Staging','Wait for the staging job queue to become ready.',true),
      node('migrate','Update staging DB','custom','Staging','Apply the reviewed schema update to staging once.'),
      node('stage-api','Stage API','deploy','Staging','Start the tested API against the updated staging database and queue.',true),
      node('stage-worker','Stage worker','deploy','Staging','Start the worker with the same staging database and queue.',true),
      node('stage-web','Stage web','deploy','Staging','Start the tested website against the healthy staging API.',true),
      node('journey','User journey test','test','Review','Exercise the website, API and queued work together in staging.'),
      node('security','Release checks','test','Review','Run the configured release checks against the staging services. This sample does not claim a security certification.'),
      node('approve','Approve release','approval','Review','A person reviews both test results, the image digests and the production plan before approving.'),
      node('prod-api','Production API','deploy','Production','Release the approved API image to the configured production destination. Production database and queue are existing prerequisites.',true),
      node('prod-worker','Production worker','deploy','Production','Release the approved worker image after the production API readiness gate. This sample assumes a compatible schema.',true),
      node('prod-web','Production web','deploy','Production','Release the approved website image after the production API is healthy.',true),
      node('verify','Verify production','test','Verify','Check the website, API and background-job path after all production services are ready.')
    ],
    edges:[
      ...['web','api','worker'].flatMap(s=>[['build-'+s,'test-'+s],['test-'+s,'push-'+s]]),
      ['database','migrate'],['push-api','migrate'],['migrate','stage-api'],['queue','stage-api'],
      ['migrate','stage-worker'],['queue','stage-worker'],['push-worker','stage-worker'],
      ['push-web','stage-web'],['stage-api','stage-web'],
      ...['stage-web','stage-api','stage-worker'].flatMap(s=>[[s,'journey'],[s,'security']]),
      ['journey','approve'],['security','approve'],['approve','prod-api'],
      ['prod-api','prod-worker'],['prod-api','prod-web'],
      ['prod-web','verify'],['prod-api','verify'],['prod-worker','verify']
    ],
    reasons:{'database>migrate':'The migration needs a healthy staging database.','push-api>migrate':'Use the migration packaged with the tested API image.','migrate>stage-api':'The staging API requires the updated schema.','migrate>stage-worker':'The worker must use the same updated schema as the API.','queue>stage-api':'The API needs the queue to submit background jobs.','queue>stage-worker':'The worker needs the queue to receive those jobs.','push-worker>stage-worker':'Use the exact worker image that passed its tests.','push-web>stage-web':'Use the exact website image that passed its tests.','stage-api>stage-web':'The staging website needs a healthy API.','journey>approve':'The reviewer needs the end-to-end user journey result.','security>approve':'The reviewer also needs the configured release-check result.','approve>prod-api':'An explicit approval releases the reviewed production plan.','prod-api>prod-worker':'This example starts the worker after the API readiness gate.','prod-api>prod-web':'The production website requires a healthy API.'}
  };
  const catalog={'web-app':medium,'saas-platform':complex};
  return {catalog,get:id=>catalog[id],title:id=>catalog[id]?.name,
    cards:()=>`<div class="dag-demo-cards">${Object.entries(catalog).map(([id,d])=>`<a class="dag-demo-card" href="#pipeline/${id}"><span class="eyebrow">${d.size.toUpperCase()} / DESIGN EXAMPLE</span><strong>${d.name}</strong><p>${d.description}</p><span>${d.nodes.length} steps · ${d.edges.length} connections <span aria-hidden="true">↗</span></span></a>`).join('')}</div>`,
    chooser:id=>`<nav class="dag-demo-picker" aria-label="Deployment examples"><span>Explore a deployment</span><a href="#pipeline/n8n" ${id==='n8n'?'aria-current="page"':''}>Small · n8n</a>${Object.entries(catalog).map(([key,d])=>`<a href="#pipeline/${key}" ${key===id?'aria-current="page"':''}>${d.size} · ${key==='web-app'?'Web app':'SaaS'}</a>`).join('')}</nav>`};
})();
