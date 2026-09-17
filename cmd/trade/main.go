package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/adshao/go-binance/v2"
)

func main() {
	if len(os.Args) < 4 {
		log.Fatalf("Usage: %s <currency> <amount_usdt> <direction(buy|sell)>\nExample: %s ETHUSDT 100 buy", os.Args[0], os.Args[0])
	}

	currency := os.Args[1]
	amount, err := strconv.ParseFloat(os.Args[2], 64)
	if err != nil {
		log.Fatalf("Invalid amount: %v", err)
	}
	direction := os.Args[3]

	apiKey := os.Getenv("BINANCE_API_KEY")
	secretKey := os.Getenv("BINANCE_SECRET_KEY")
	if apiKey == "" || secretKey == "" {
		log.Fatal("BINANCE_API_KEY and BINANCE_SECRET_KEY must be set")
	}

	ctx := context.Background()
	client := binance.NewClient(apiKey, secretKey)

	prices, err := client.NewListPricesService().Symbol(currency).Do(ctx)
	if err != nil {
		log.Fatalf("Failed to get price: %v", err)
	}
	if len(prices) == 0 {
		log.Fatalf("No price found for %s", currency)
	}
	currentPrice, err := strconv.ParseFloat(prices[0].Price, 64)
	if err != nil {
		log.Fatalf("Invalid price: %v", err)
	}

	quantity := amount / currentPrice

	// Get symbol info to determine quantity precision
	info, err := client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		log.Fatalf("Failed to get exchange info: %v", err)
	}
	var stepSize string
	for _, s := range info.Symbols {
		if s.Symbol == currency {
			for _, f := range s.Filters {
				if f["filterType"] == "LOT_SIZE" {
					stepSize = f["stepSize"].(string)
					break
				}
			}
			break
		}
	}
	if stepSize == "" {
		log.Fatalf("Could not determine step size for %s", currency)
	}

	stepPrecision := 0
	if idx := strings.Index(stepSize, "."); idx != -1 {
		stepPrecision = len(stepSize) - idx - 1
		// trim trailing zeros
		for stepPrecision > 0 && stepSize[len(stepSize)-1] == '0' {
			stepSize = stepSize[:len(stepSize)-1]
			stepPrecision--
		}
	}
	quantityPrecision := stepPrecision
	if quantityPrecision < 0 {
		quantityPrecision = 0
	}
	quantityStr := strconv.FormatFloat(quantity, 'f', quantityPrecision, 64)

	var side binance.SideType
	switch direction {
	case "buy":
		side = binance.SideTypeBuy
	case "sell":
		side = binance.SideTypeSell
	default:
		log.Fatalf("Invalid direction: %s (use buy or sell)", direction)
	}

	order, err := client.NewCreateOrderService().
		Symbol(currency).
		Side(side).
		Type(binance.OrderTypeMarket).
		Quantity(quantityStr).
		Do(ctx)

	if err != nil {
		log.Fatalf("Failed to place order: %v", err)
	}

	fmt.Printf("Order placed successfully:\n")
	fmt.Printf("  Symbol:    %s\n", order.Symbol)
	fmt.Printf("  Side:      %s\n", order.Side)
	fmt.Printf("  Type:      %s\n", order.Type)
	fmt.Printf("  Quantity:  %s\n", order.OrigQuantity)
	fmt.Printf("  Price:     %s\n", order.Price)
	fmt.Printf("  Status:    %s\n", order.Status)
	fmt.Printf("  OrderID:   %d\n", order.OrderID)
}
