package main

import (
	"context"
	"fmt"
	"log"
	"net"

	pb "go-flashsale/product-service/proto"

	// Kita tambahkan driver redis/v9 ke dalam import
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Definisi Struktur Tabel DB (ORM GORM)
type Product struct {
	ID    uint   `gorm:"primaryKey"`
	Name  string `gorm:"type:varchar(100)"`
	Price int32  // Menggunakan int64 agar sinkron dengan file proto gRPC
	Stock int32
}

type server struct {
	pb.UnimplementedProductServiceServer
	db *gorm.DB
}

// Fungsi gRPC GetProduct membaca langsung dari Postgres
func (s *server) GetProduct(ctx context.Context, req *pb.ProductRequest) (*pb.ProductResponse, error) {
	var p Product
	result := s.db.First(&p, req.GetId())
	if result.Error != nil {
		return nil, fmt.Errorf("produk dengan ID %d tidak ditemukan di DB", req.GetId())
	}

	return &pb.ProductResponse{
		Id:    int32(p.ID),
		Name:  p.Name,
		Price: p.Price,
		Stock: p.Stock,
	}, nil
}

func main() {
	ctx := context.Background()

	// 1. Koneksi ke Postgres Docker
	dsn := "host=127.0.0.1 user=postgres password=secretpassword dbname=flashsale_db port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Gagal koneksi ke Postgres: %v", err)
	}

	// Auto Migrate: Otomatis bikin tabel 'products' kalau belum ada di DB
	db.AutoMigrate(&Product{})

	// 2. Koneksi ke Redis dari Product Service
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})

	// Isi data awal (seed) ke Postgres jika DB masih kosong
	var count int64
	db.Model(&Product{}).Count(&count)
	if count == 0 {
		db.Create(&Product{ID: 1, Name: "Sepeda Balap Carbon Riil DB", Price: 15000000, Stock: 1000})
		log.Println("Seed data produk ID 1 berhasil dimasukkan ke Postgres!")
	}

	// 🌟 SINKRONISASI CACHE: Set/Reset stok awal ke Redis Cache sebesar 1000 setiap aplikasi restart
	var productFromDB Product
	// Ambil data produk ID 1 dari Postgres
	if err := db.First(&productFromDB, 1).Error; err != nil {
		log.Printf("Peringatan: Produk ID 1 belum ada di Postgres. Gagal sinkronisasi ke Redis.")
	} else {
		// Set stok ke Redis menggunakan nilai 'productFromDB.Stock' asli dari database!
		err = rdb.Set(ctx, "product_stock:1", productFromDB.Stock, 0).Err()
		if err != nil {
			log.Fatalf("Gagal set stok ke Redis: %v", err)
		}
		log.Printf("Stok produk ID 1 BERHASIL disinkronkan dari Postgres ke Redis Cache sebesar: %d", productFromDB.Stock)
	}

	// 3. Jalankan gRPC Server
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Gagal listen port 50051: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterProductServiceServer(s, &server{db: db})

	log.Println("Product gRPC Service (REAL DB + CACHE) berjalan di port :50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Gagal menjalankan server: %v", err)
	}
}
