const handler = async (event) => {
  console.log(event);
  return {
    statusCode: 200,
    body: JSON.stringify({
      message: 'Hello, World! You have hit the qrioso-serverless API endpoint',
    }),
  };
};


export default handler;