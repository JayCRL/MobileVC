import asyncio, json
import websockets

async def main():
    async with websockets.connect('ws://127.0.0.1:8081/ws?token=test') as ws:
        for _ in range(2):
            print('INIT', await ws.recv())
        await ws.send(json.dumps({'action': 'exec', 'mode': 'pty', 'cmd': 'claude'}))
        sent = False
        for i in range(30):
            msg = await asyncio.wait_for(ws.recv(), timeout=20)
            print('EVT', msg)
            data = json.loads(msg)
            if not sent and data.get('type') == 'agent_state' and data.get('state') == 'WAIT_INPUT':
                await ws.send(json.dumps({'action': 'input', 'data': '你好\n'}))
                print('SENT_INPUT')
                sent = True

asyncio.run(main())
