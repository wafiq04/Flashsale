package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	pb "go-flashsale/product-service/proto"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var rdb *redis.Client

func main() {
	// 1. Inisialisasi Koneksi Redis
	rdb = redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
	})

	// 2. Setup gRPC Client ke Product Service
	conn, err := grpc.Dial("127.0.0.1:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Gagal terhubung ke Product Service: %v", err)
	}
	defer conn.Close()
	productClient := pb.NewProductServiceClient(conn)

	// 3. Setup Gin
	r := gin.Default()

	// ─── ROUTE 1: GET UNTUK AMBIL DATA PRODUK (TEST CASE 1, 2, 3) ───
	r.GET("/order/:id", func(c *gin.Context) {
		idStr := c.Param("id")
		productID, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ID harus berupa angka"})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Panggil Product Service via gRPC (Sekarang membaca Real DB!)
		product, err := productClient.GetProduct(ctx, &pb.ProductRequest{Id: int32(productID)})
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "Gagal mengambil data produk",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "Order siap diproses",
			"product": product,
		})
	})

	// Route POST untuk Checkout (Simulasi Flash Sale)
	// ─── ROUTE 2: POST UNTUK CHECKOUT FLASH SALE (OPTIMIZED VIA REDIS CACHE) ───
	type CheckoutRequest struct {
		ProductID int64 `json:"product_id"`
	}
	r.POST("/checkout", func(c *gin.Context) {
		var req CheckoutRequest

		// 🌟 1. Tangkap data JSON dari k6 dan cek detail error-nya di sini
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("[ERROR] Gagal membaca JSON request: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Format request salah, harus JSON",
				"details": err.Error(),
			})
			return
		}

		// Konversi ID angka ke string untuk key Redis
		productIDStr := strconv.FormatInt(req.ProductID, 10)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Potong stok langsung di Redis (Atomic Decrement)
		newStock, err := rdb.Decr(ctx, "product_stock:"+productIDStr).Result()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memproses stok di cache"})
			return
		}

		// Jika setelah dikurangi nilainya kurang dari 0, berarti stock aslinya sudah habis!
		if newStock < 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "Gagal",
				"message": "Maaf, Produk ini sudah habis diserbu pembeli!",
				"debug":   "Sisa key " + productIDStr + " di Redis bernilai minus",
			})
			return
		}

		// Lempar ke Redis Stream Antrean
		err = rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: "flashsale_orders",
			Values: map[string]interface{}{
				"product_id":   productIDStr,
				"product_name": "Sepeda Balap Carbon Riil DB",
				"user_id":      "user_random_123",
			},
		}).Err()

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal masuk antrean"})
			return
		}

		c.JSON(http.StatusAccepted, gin.H{
			"status":  "Antrean Dibuat",
			"message": "Pesanan Anda aman! Sedang diproses di latar belakang.",
		})
	})

	// 4. Jalankan Worker di Latar Belakang (Goroutine)
	// Kata kunci 'go' di bawah ini yang membuat fungsi berjalan secara asinkron paralel!
	go startStockWorker()

	log.Println("Order HTTP Service berjalan di port :8080...")
	r.Run(":8080")
}
