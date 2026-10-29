export default function health(request, response) {
  const now = new Date().toISOString();
  console.info(JSON.stringify({service:'cooker-example-service',event:'health_check',timestamp:now}));
  response.setHeader('Cache-Control','no-store');
  response.status(200).json({service:'cooker-example-service',status:'ok',timestamp:now});
}
