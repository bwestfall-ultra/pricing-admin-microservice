package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
)

var ctx = context.Background()

type ProductPrice struct {
	PriceListID string      `json:"price_list_id"`
	SKUID       string      `json:"sku_id"`
	Currency    string      `json:"currency"`
	StartDate   string      `json:"start_date"` // nil = beginning of time
	EndDate     string      `json:"end_date"`   // nil = forever
	BasePrice   float64     `json:"base_price"`
	TierPrice   []TierPrice `json:"tier_price"`
}

type TierPrice struct {
	Qty       int     `json:"qty"`
	BasePrice float64 `json:"base_price"`
}

type Tier struct {
	Qty   int
	Price string
}

type PriceModifier struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`
	Conditions   map[string]string `json:"conditions"`
	Adjustment   float64           `json:"adjustment"`
	RateType     string            `json:"rate_type"`
	ExcludedSkus []string          `json:"excluded_skus"`
	IncludedSkus []string          `json:"included_skus"`
	Status       string            `json:"status"`
}

type ModifierResponse struct {
	Pricelist []PriceModifier `json:"modifiers"`
}

type PricelistResponse struct {
	Pricelist []string `json:"pricelists"`
}

type SkuResponse struct {
	Sku []string `json:"skus"`
}

var rdb *redis.Client

func main() {
	rdb = redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "mypassword",
		DB:       0,
	})

	router := mux.NewRouter()

	// Pricing endpoints
	router.HandleFunc("/prices", AddOrUpdatePriceHandler).Methods("POST")
	router.HandleFunc("/prices", DeletePriceHandler).Methods("DELETE")
	router.HandleFunc("/prices/{pricelist}/{sku}/{currency}", GetPricesHandler).Methods("GET")
	router.HandleFunc("/prices/pricelists", GetPriceslistHandler).Methods("GET")
	router.HandleFunc("/prices/skus", GetSkusHandler).Methods("GET")
	router.HandleFunc("/prices/upload-prices", UploadPricesHandler(rdb)).Methods("POST")

	// Modifier endpoints
	router.HandleFunc("/modifiers", AddOrUpdateModifierHandler).Methods("POST")
	router.HandleFunc("/modifiers/{id}", DeleteModifierHandler).Methods("DELETE")
	router.HandleFunc("/modifiers", ListModifiersHandler).Methods("GET")

	log.Println("Pricing admin API running on :8083")
	log.Fatal(http.ListenAndServe(":8083", router))
}

// ---- PRICE HANDLERS ----

func AddOrUpdatePriceHandler(w http.ResponseWriter, r *http.Request) {
	var price ProductPrice

	if err := json.NewDecoder(r.Body).Decode(&price); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		log.Println(err.Error())
		return
	}

	if price.SKUID == "" {
		http.Error(w, "Sku ID (sku_id) is required", http.StatusBadRequest)
		return
	}

	if price.PriceListID == "" {
		http.Error(w, "Pricelist (price_list_id) is required", http.StatusBadRequest)
		return
	}

	if price.Currency == "" {
		http.Error(w, "Currency (currency) is required", http.StatusBadRequest)
		return
	}

	if price.StartDate == "" {
		log.Println("No start date provided, setting to 1900-01-01")
		price.StartDate = "1900-01-01"
	} else {
		if !isValidDate(price.StartDate) {
			http.Error(w, "StartDate (start_date) is not valid (YYYY-MM-DD)", http.StatusBadRequest)
			return

		}

	}

	if price.EndDate != "" {
		if !isValidDate(price.EndDate) {
			http.Error(w, "EndDate (end_date) is not valid (YYYY-MM-DD)", http.StatusBadRequest)
			return
		}

	}

	log.Println(price.StartDate)

	// if price.BasePrice <= 0 {
	// 	http.Error(w, "Base Price (currency) is required", http.StatusBadRequest)
	// 	return
	// }

	key := fmt.Sprintf("price:%s:%s:%s:%s", price.PriceListID, price.SKUID, price.Currency, price.StartDate)

	priceKey := fmt.Sprintf("price:%s:%s:%s", price.PriceListID, price.SKUID, price.Currency)

	hashFields := FormatTierPricesForRedis(price.TierPrice)
	hashFields["end_date"] = price.EndDate

	log.Println(hashFields)

	if err := rdb.HSet(ctx, key, hashFields).Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	start, _ := time.Parse("2006-01-02", price.StartDate)

	rdb.ZAdd(ctx, priceKey, &redis.Z{
		Score:  float64(start.Unix()),
		Member: key,
	})

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Price added or updated"))
}

func DeletePriceHandler(w http.ResponseWriter, r *http.Request) {
	var price ProductPrice

	if err := json.NewDecoder(r.Body).Decode(&price); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("price:%s:%s:%s:%s", price.PriceListID, price.SKUID, price.Currency, price.StartDate)

	if err := rdb.Del(ctx, key).Err(); err != nil {
		http.Error(w, "Failed to delete price", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Price deleted"))
}

func GetSkusHandler(w http.ResponseWriter, r *http.Request) {
	key := "price:*:*:*"
	pattern := key //"price:*:*:*"
	var cursor uint64
	skuSet := make(map[string]bool)

	var keys []string
	var err error

	for {
		var batch []string
		batch, cursor, err = rdb.Scan(ctx, cursor, pattern, 100).Result()

		if err != nil {
			log.Fatalf("Failed to scan keys: %v", err)
		}
		keys = append(keys, batch...)

		if cursor == 0 {
			break
		}
	}

	for _, v := range keys {
		parts := strings.Split(v, ":")
		log.Println(parts)
		if len(parts) == 4 {
			skulist := parts[2]
			skuSet[skulist] = true
		}
	}

	var distinctSkus []string
	for pl := range skuSet {
		distinctSkus = append(distinctSkus, pl)
	}

	sort.Strings(distinctSkus)

	fmt.Println("Distinct SKUs:")
	for _, pl := range distinctSkus {
		fmt.Println("  -", pl)
	}
	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(SkuResponse{distinctSkus})
}

func GetPriceslistHandler(w http.ResponseWriter, r *http.Request) {
	key := "price:*:*:*:*"
	pattern := key //"price:*:*:*:*"
	var cursor uint64
	pricelistSet := make(map[string]bool)

	var keys []string
	var err error

	for {
		var batch []string
		batch, cursor, err = rdb.Scan(ctx, cursor, pattern, 100).Result()

		if err != nil {
			log.Fatalf("Failed to scan keys: %v", err)
		}
		keys = append(keys, batch...)

		if cursor == 0 {
			break
		}
	}

	for _, v := range keys {
		parts := strings.Split(v, ":")
		log.Println(parts)
		if len(parts) < 5 {
			continue // not a valid key
		}
		pricelist := parts[1]
		pricelistSet[pricelist] = true
	}

	var distinctPricelists []string
	for pl := range pricelistSet {
		distinctPricelists = append(distinctPricelists, pl)
	}

	sort.Strings(distinctPricelists)

	fmt.Println("Distinct pricelists:")
	for _, pl := range distinctPricelists {
		fmt.Println("  -", pl)
	}
	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(PricelistResponse{distinctPricelists})
}

func GetPricesHandler(w http.ResponseWriter, r *http.Request) {
	sku := mux.Vars(r)["sku"]
	pricelist := mux.Vars(r)["pricelist"]
	currency := mux.Vars(r)["currency"]
	key := fmt.Sprintf("price:%s:%s:%s:*", pricelist, sku, currency)

	pattern := key //"price:*:8a82a4ab90e2be830190e34a78751337:*:*"

	log.Println(pattern)
	var cursor uint64
	var keys []string
	var err error

	for {
		var batch []string
		batch, cursor, err = rdb.Scan(ctx, cursor, pattern, 100).Result()

		if err != nil {
			log.Fatalf("Failed to scan keys: %v", err)
		}
		keys = append(keys, batch...)

		if cursor == 0 {
			break
		}
	}

	var prices []ProductPrice
	for _, v := range keys {
		log.Println(v)
		val, err := rdb.HGetAll(ctx, v).Result()
		if err != nil {
			continue
		}

		log.Println(val)

		tierPrices, err := decodeTierPriceMap(val)

		if err != nil {

		}

		parts := strings.Split(v, ":")

		prices = append(prices, ProductPrice{SKUID: parts[2], Currency: parts[3], BasePrice: tierPrices[0].BasePrice, PriceListID: parts[1], StartDate: parts[4], EndDate: val["end_date"], TierPrice: tierPrices})

	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prices)
}

// ---- MODIFIER HANDLERS ----

func AddOrUpdateModifierHandler(w http.ResponseWriter, r *http.Request) {
	var mod PriceModifier
	if err := json.NewDecoder(r.Body).Decode(&mod); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if mod.Type == "" {
		http.Error(w, "Modifier Type (type) is Required", http.StatusBadRequest)
		return
	}

	if mod.ID == "" {
		http.Error(w, "Modifier ID (id) is Required", http.StatusBadRequest)
		return
	}

	if mod.RateType == "" {
		http.Error(w, "Modifier Rate Type (rate_type) is Required", http.StatusBadRequest)
		return
	}

	if mod.Conditions == nil {
		http.Error(w, "Modifier Condtions (conditions) is Required", http.StatusBadRequest)
		return
	}

	// Set Status to Active by Default if not provided
	if mod.Status == "" {
		mod.Status = "active"
	}

	if len(mod.ExcludedSkus) > 0 && len(mod.IncludedSkus) > 0 {
		http.Error(w, "Cannot use both included and excluded SKUs", http.StatusBadRequest)
		return
	}

	data, _ := json.Marshal(mod)
	key := fmt.Sprintf("modifier:%s", mod.ID)

	pipe := rdb.TxPipeline()
	pipe.Set(ctx, key, data, 0)
	pipe.SAdd(ctx, "modifiers", mod.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		http.Error(w, "Failed to save modifier", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Modifier added or updated"))
}

func DeleteModifierHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	key := fmt.Sprintf("modifier:%s", id)

	pipe := rdb.TxPipeline()
	pipe.Del(ctx, key)
	pipe.SRem(ctx, "modifiers", id)
	if _, err := pipe.Exec(ctx); err != nil {
		http.Error(w, "Failed to delete modifier", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Modifier deleted"))
}

func ListModifiersHandler(w http.ResponseWriter, r *http.Request) {
	ids, err := rdb.SMembers(ctx, "modifiers").Result()
	//rdb.Get(ctx, "modifiers:"+skuID).Result()
	if err != nil {
		http.Error(w, "Failed to list modifiers", http.StatusInternalServerError)
		return
	}
	log.Println(len(ids))
	var mods []PriceModifier
	for _, id := range ids {
		log.Println(id)
		val, err := rdb.Get(ctx, fmt.Sprintf("modifier:%s", id)).Result()
		if err != nil {
			continue
		}

		var mod PriceModifier
		if err := json.Unmarshal([]byte(val), &mod); err == nil {
			mods = append(mods, mod)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ModifierResponse{mods}) //Test formatting

}

func decodeTierPriceMap(data map[string]string) ([]TierPrice, error) {
	var tiers []TierPrice

	for k, v := range data {
		log.Println(k)

		if k == "end_date" || k == "currency" || k == "start_ts" || k == "end_ts" {
			continue
		}

		qty, err := strconv.Atoi(k)
		if err != nil {
			return nil, fmt.Errorf("invalid quantity key %q: %v", k, err)
		}

		price, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid price value %q: %v", v, err)
		}

		tiers = append(tiers, TierPrice{
			Qty:       qty,
			BasePrice: price,
		})

	}

	// Optional: sort by Qty ascending
	sort.Slice(tiers, func(i, j int) bool {
		return tiers[i].Qty < tiers[j].Qty
	})

	return tiers, nil
}

func FormatTierPricesForRedis(tiers []TierPrice) map[string]interface{} {
	data := make(map[string]interface{})
	for _, tier := range tiers {
		key := fmt.Sprintf("%d", tier.Qty)
		value := fmt.Sprintf("%.2f", tier.BasePrice) // or just `tier.BasePrice` as float64
		data[key] = value
	}
	return data
}

func isValidDate(dateStr string) bool {
	_, err := time.Parse("2006-01-02", dateStr)
	return err == nil
}

func UploadPricesHandler(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()

		err := r.ParseMultipartForm(500 << 20) // 10MB max
		if err != nil {
			http.Error(w, "Invalid multipart form", http.StatusBadRequest)
			return
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Missing file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		reader := csv.NewReader(file)
		headers, err := reader.Read()
		if err != nil {
			http.Error(w, "Invalid CSV header", http.StatusBadRequest)
			return
		}

		for {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				continue
			}

			row := map[string]string{}
			for i, h := range headers {
				row[h] = record[i]
			}

			priceKey := fmt.Sprintf("price:%s:%s:%s:%s", row["price_list_id"], row["sku_id"], row["currency"], row["start_date"])

			data := map[string]interface{}{
				"end_date": row["end_date"],
			}

			// Add tier prices
			type Tier struct {
				Qty   int
				Price string
			}

			var tiers []Tier
			tierMap := make(map[string]Tier)

			for _, header := range headers {
				if strings.HasPrefix(header, "tier_qty_") {
					suffix := strings.TrimPrefix(header, "tier_qty_")
					qtyStr := row[header]
					priceStr := row["tier_price_"+suffix]

					if qtyStr != "" && priceStr != "" {
						qty, err := strconv.Atoi(qtyStr)
						if err != nil {
							continue
						}
						tierMap[suffix] = Tier{Qty: qty, Price: priceStr}
					}
				}
			}

			// Collect tiers
			for _, tier := range tierMap {
				tiers = append(tiers, tier)
			}

			// Sort tiers by Qty ascending
			sort.Slice(tiers, func(i, j int) bool {
				return tiers[i].Qty < tiers[j].Qty
			})

			// Add to data for Redis
			for _, t := range tiers {
				data[strconv.Itoa(t.Qty)] = t.Price
			}

			log.Println(data)

			if err := rdb.HSet(ctx, priceKey, data).Err(); err != nil {
				fmt.Println("HSET error:", err)
				continue
			}

		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Prices uploaded successfully"))
	}
}
