import asyncio
from pepperstone_trade import start_ctrader, place_market_order

async def main():
    start_ctrader()
    await asyncio.sleep(2)  # холболт + auth хийгдэх хүртэл хүлээх

    # Таны анализ:
    signal = "buy"      # эсвэл "sell"
    pair = "USDJPY"
    lot = 0.01

    result = await place_market_order(lot, signal, pair)
    print("Захиалгын хариу:", result)

if __name__ == "__main__":
    asyncio.run(main())