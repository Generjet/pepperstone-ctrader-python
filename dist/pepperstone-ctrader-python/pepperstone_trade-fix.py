from twisted.internet import reactor
import json
from ctrader_fix import LogonRequest, Client, NewOrderSingle

# Callback: мессеж ирэхэд
def onMessageReceived(client, responseMessage):
    print("Ирсэн мессеж:", responseMessage.getMessage().replace("\x01", "|"))
    messageType = responseMessage.getFieldValue(35)
    if messageType == "A":
        print("✅ Амжилттай нэвтэрлээ!")
        # Нэвтэрсний дараа захиалга илгээх
        send_market_order(client)

# Callback: холбогдоход
def connected(client):
    print("Холбогдлоо. Logon илгээж байна...")
    logonRequest = LogonRequest(config)
    client.send(logonRequest)

# Callback: салахад
def disconnected(client, reason):
    print("Саллаа. Шалтгаан:", reason)

# Захиалга илгээх функц
def send_market_order(client):
    order = NewOrderSingle(
        clOrdID="order_001",
        symbol="USDJPY",        # ⚠️ FIX Symbol ID биш, нэрээр эсвэл ID-аар
        side="1",               # 1=BUY, 2=SELL
        orderQty=1000,          # ⚠️ 0.01 лот = 1000 units
        ordType="1",            # 1=MARKET
        transactTime="20260911-12:00:00"
    )
    client.send(order)
    print("📤 Захиалга илгээгдлээ: BUY 0.01 USDJPY")

# Config уншиж, клиент эхлүүлэх
with open("config-trade.json") as f:
    config = json.load(f)

client = Client(config["Host"], config["Port"], ssl=config["SSL"])
client.setConnectedCallback(connected)
client.setDisconnectedCallback(disconnected)
client.setMessageReceivedCallback(onMessageReceived)

client.startService()
reactor.run()