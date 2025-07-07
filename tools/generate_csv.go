package main

import (
	"encoding/csv"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"
)

type PriceWindow struct {
	Label     string
	StartDate string
	EndDate   string
}

func generateDateWindows() map[string]PriceWindow {
	today := time.Now()

	return map[string]PriceWindow{
		"expired": {
			Label:     "expired",
			StartDate: today.AddDate(-2, 0, 0).Format("2006-01-02"),
			EndDate:   today.AddDate(-1, 0, 0).Format("2006-01-02"),
		},
		"active": {
			Label:     "active",
			StartDate: today.AddDate(-1, 0, 0).Format("2006-01-02"),
			EndDate:   today.AddDate(1, 0, 0).Format("2006-01-02"),
		},
		"future": {
			Label:     "future",
			StartDate: today.AddDate(1, 0, 1).Format("2006-01-02"),
			EndDate:   today.AddDate(2, 0, 0).Format("2006-01-02"),
		},
	}
}

func main() {
	file, err := os.Create("pricing_tiers.csv")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// CSV Header
	header := []string{
		"sku_id", "price_list_id", "currency", "base_price", "start_date", "end_date","min_sale_price",
		"tier_qty_1", "tier_price_1", "tier_qty_2", "tier_price_2",
		"tier_qty_3", "tier_price_3", "tier_qty_4", "tier_price_4",
	}
	writer.Write(header)

	rand.Seed(time.Now().UnixNano())

	priceLists := []string{"default", "wholesale", "b2b"}
	numSKUs := 10000
	dateWindows := generateDateWindows()

	for i := 0; i < numSKUs; i++ {
		skuID := fmt.Sprintf("SKU%010d", rand.Intn(1000000000))
		currency := "USD"

		for _, priceList := range priceLists {
			usedWindows := make(map[string]bool)

			// Always include an active price row
			writePriceRow(writer, skuID, priceList, currency, dateWindows["active"])
			usedWindows["active"] = true

			// Randomly include 0–2 more rows (expired/future)
			extras := []string{"expired", "future"}
			rand.Shuffle(len(extras), func(i, j int) {
				extras[i], extras[j] = extras[j], extras[i]
			})

			numExtras := rand.Intn(3) // could be 0, 1, or 2 extra rows
			for j := 0; j < numExtras; j++ {
				label := extras[j]
				if !usedWindows[label] {
					writePriceRow(writer, skuID, priceList, currency, dateWindows[label])
					usedWindows[label] = true
				}
			}
		}
	}

	fmt.Println("CSV file 'pricing_tiers.csv' generated successfully.")
}

func writePriceRow(writer *csv.Writer, skuID, priceList, currency string, window PriceWindow) {
	basePrice := rand.Intn(151) + 50 // $50–$200 whole dollars
	min_sale_price := (basePrice * 60) / 100

	record := []string{
		skuID,
		priceList,
		currency,
		strconv.Itoa(basePrice),
		window.StartDate,
		window.EndDate,
		strconv.Itoa(min_sale_price),
	}

	// Always include tier 1: qty=1, price=basePrice
	record = append(record, "1", strconv.Itoa(basePrice))

	// Add 1–3 more tiers
	tierCount := rand.Intn(3) + 1
	tierQty := 10
	for t := 0; t < 3; t++ {
		if t < tierCount {
			tierPrice := basePrice - ((t + 1) * 5)
			if tierPrice < 1 {
				tierPrice = 1
			}
			record = append(record, strconv.Itoa(tierQty), strconv.Itoa(tierPrice))
			tierQty += 15
		} else {
			record = append(record, "", "")
		}
	}

	writer.Write(record)
}
