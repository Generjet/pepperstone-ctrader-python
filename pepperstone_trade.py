import asyncio
from ctrader_open_api import Client, Protobuf, TcpProtocol, EndPoints
from ctrader_open_api.messages.OpenApiMessages_pb2 import (
    ProtoOAAccountAuthReq,
    ProtoOANewOrderReq,
    ProtoOAApplicationAuthReq,
)
from ctrader_open_api.messages.OpenApiModelMessages_pb2 import (
    ProtoOATradeSide,
    ProtoOAOrderType,
)

# ========== ТОХИРГОО (энэ хэсгийг өөрийн утгуудаар солино) ==========
CLIENT_ID = "your_client_id"
CLIENT_SECRET = "your_client_secret"
ACCESS_TOKEN = "your_access_token"
ACCOUNT_ID = 12345678  # ctidTraderAccountId (тоо)
HOST = "demo.ctraderapi.com"  # Demo; live: live.ctraderapi.com
PORT = 5035
# ====================================================================

_client = None
_authenticated = False
_order_future = None  # захиалгын хариу хүлээх future

async def _on_connected(client):
    """Холбогдсоны дараа хоёр шатлалт баталгаажуулалт хийнэ"""
    global _authenticated

    # 1) Application auth
    app_req = ProtoOAApplicationAuthReq()
    app_req.clientId = CLIENT_ID
    app_req.clientSecret = CLIENT_SECRET
    await client.send(app_req)

    # 2) Account auth
    acc_req = ProtoOAAccountAuthReq()
    acc_req.ctidTraderAccountId = ACCOUNT_ID
    acc_req.accessToken = ACCESS_TOKEN
    await client.send(acc_req)

    _authenticated = True
    print("[Auth] Баталгаажуулалт илгээгдлээ.")

def _on_message(client, message):
    """Бүх ирж буй мессежийг сонсож, захиалгын хариуг future-т хийнэ"""
    global _order_future
    parsed = Protobuf.extract(message)
    print(f"[Message] {parsed}")

    # Хэрэв энэ нь захиалгын хариу (execution event) бол future-ийг дуусгана
    if _order_future and not _order_future.done():
        # энгийнээр бүх мессежийг хариу гэж үзэж байна (бодит кодонд payloadType-оор шалга)
        _order_future.set_result(parsed)

def _on_disconnected(client, reason):
    print(f"[Disconnected] {reason}")

def start_ctrader():
    """cTrader холболтыг эхлүүлнэ. main() эхэнд нэг удаа дуудна."""
    global _client
    _client = Client(HOST, PORT, TcpProtocol)
    _client.setConnectedCallback(lambda c: asyncio.ensure_future(_on_connected(c)))
    _client.setMessageReceivedCallback(_on_message)
    _client.setDisconnectedCallback(_on_disconnected)
    _client.startService()
    print("[Init] cTrader Open API холболт эхэллээ.")

async def place_market_order(amount, side, symbol):
    """
    Market order илгээж, хариуг буцаана.
    :param amount: лот (жишээ нь 0.01)
    :param side: "buy" эсвэл "sell"
    :param symbol: "USDJPY" гэх мэт
    :return: серверийн хариу (parsed protobuf) эсвэл алдааны мэдээлэл
    """
    global _order_future

    if _client is None or not _authenticated:
        return "Error: cTrader холбогдоогүй эсвэл баталгаажаагүй."

    # ⚠️ ЧУХАЛ: лот → volume units
    # 0.01 лот = 1000 units (стандарт forex)
    volume_units = int(amount * 100_000)

    order = ProtoOANewOrderReq()
    order.ctidTraderAccountId = ACCOUNT_ID
    order.symbolName = symbol
    order.volume = volume_units
    order.orderType = ProtoOAOrderType.MARKET
    order.tradeSide = (
        ProtoOATradeSide.BUY if side.lower() == "buy" else ProtoOATradeSide.SELL
    )

    # Хариу хүлээх future
    loop = asyncio.get_event_loop()
    _order_future = loop.create_future()

    await _client.send(order)
    print(f"[Order] {side.upper()} {amount} лот ({volume_units} units) {symbol} илгээгдлээ.")

    # Хариу ирэх хүртэл хүлээх (timeout-той)
    try:
        result = await asyncio.wait_for(_order_future, timeout=10.0)
        return result
    except asyncio.TimeoutError:
        return "Error: Захиалгын хариу 10 секундэд ирсэнгүй."