package main

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Product struct {
	ID    uint
	Name  string
	Price int64
	Stock int32
}

func startStockWorker() {
	ctx := context.Background()

	dsn := "host=127.0.0.1 user=postgres password=secretpassword dbname=flashsale_db port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("[WORKER ERROR] Gagal koneksi ke Postgres: %v", err)
	}

	log.Println("Background Stock Worker (REAL DB) aktif mendengarkan antrean...")

	for {
		// Ambil 1 pesan dari antrean paling awal ("0")
		streams, err := rdb.XRead(ctx, &redis.XReadArgs{
			Streams: []string{"flashsale_orders", "0"},
			Block:   0,
			Count:   1,
		}).Result()

		if err != nil {
			// Jika antrean benar-benar kosong, dia akan log error nil/redis.Nil, kita sleep sebentar
			time.Sleep(100 * time.Millisecond)
			continue
		}

		for _, stream := range streams {
			for _, message := range stream.Messages {
				pID := message.Values["product_id"]
				uID := message.Values["user_id"]

				log.Printf("[WORKER] MEMPROSES ANTRIAN: User %v membeli Produk ID: %v", uID, pID)

				// 1. EKSEKUSI REAL UPDATE KE POSTGRES (Hanya kurangi jika stock > 0)
				result := db.Model(&Product{}).Where("id = ? AND stock > 0", pID).Update("stock", gorm.Expr("stock - 1"))

				if result.Error != nil {
					log.Printf("[WORKER ERROR] Gagal update stok di DB: %v", result.Error)
					continue
				}

				if result.RowsAffected == 0 {
					log.Printf("[WORKER GAGAL] Produk ID %v gagal dibeli. Alasan: STOK HABIS DI DB!", pID)
				} else {
					log.Printf("[WORKER SUKSES] Stok Produk ID %v berhasil dikurangi 1 di Postgres!", pID)
				}

				// 🌟 KUNCI SINKRONISASI: Hapus pesan dari Redis Stream setelah diproses!
				// Perintah XDel ini akan mengurangi jumlah XLEN secara riil di Redis
				rdb.XDel(ctx, "flashsale_orders", message.ID)
			}
		}
	}
}
