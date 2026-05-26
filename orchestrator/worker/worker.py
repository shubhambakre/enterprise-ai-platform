import asyncio
import json
import logging
from nats.aio.client import Client as NATS

logging.basicConfig(level=logging.INFO, format='%(asctime)s [%(levelname)s] %(message)s')

async def run():
    nc = NATS()
    
    # Connect to NATS server
    await nc.connect("nats://localhost:4222")
    logging.info("Python Worker connected to NATS successfully.")

    async def message_handler(msg):
        subject = msg.subject
        reply = msg.reply
        data = json.loads(msg.data.decode())
        
        logging.info(f"Received task on '{subject}': {data['id']} (Type: {data['type']})")
        
        # Simulate agentic work (e.g. LLM inference, API calls, tool usage)
        await asyncio.sleep(1.5)
        
        result = f"Worker completed {data['id']}. Result: Simulated output for {data['input']}"
        logging.info(f"Completed task {data['id']}. Sending reply...")
        
        # Send the response back to the Go Router via the reply subject
        await nc.publish(reply, result.encode())

    # Subscribe to the tasks queue
    # Using a queue group 'workers' means multiple python processes can load balance the tasks
    sub = await nc.subscribe("agent.tasks", queue="workers", cb=message_handler)
    logging.info("Listening for tasks on 'agent.tasks'...")

    # Keep the worker running
    try:
        await asyncio.Event().wait()
    except KeyboardInterrupt:
        pass
    finally:
        await sub.unsubscribe()
        await nc.close()

if __name__ == '__main__':
    asyncio.run(run())
