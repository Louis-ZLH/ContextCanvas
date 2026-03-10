package app

import (
	"context"
	"log"
	"strings"

	"github.com/luhao/contextGraph/config"
	"github.com/luhao/contextGraph/internal/consumer"
	"github.com/luhao/contextGraph/internal/handler"
	"github.com/luhao/contextGraph/internal/infra"
	"github.com/luhao/contextGraph/internal/migrate"
	"github.com/luhao/contextGraph/internal/repo"
	"github.com/luhao/contextGraph/internal/service"
	"github.com/luhao/contextGraph/pkg/email"
	"github.com/luhao/contextGraph/pkg/idgen"
	"github.com/luhao/contextGraph/pkg/utils"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type App struct {
	Cfg   *config.Config
	DB    *gorm.DB
	RDB   *redis.Client
	Minio *minio.Client
	MQ    *infra.RabbitMQ
	AI    *infra.AIClient

	EmailConsumer *consumer.EmailConsumer
	H             *Handlers
}

func New(cfg *config.Config) (*App, error) {

	// 1. MySQL
	db, err := infra.NewMySQL(cfg.MySQLDSN)
	if err != nil {
		return nil, err
	}

	// 2. Redis
	rdb, err := infra.NewRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}

	// 3. MinIO
	minioClient, err := infra.NewMinio(infra.MinioConfig{
		Endpoint:  cfg.MinioEndpoint,
		AccessKey: cfg.MinioAccessKey,
		SecretKey: cfg.MinioSecretKey,
		UseSSL:    cfg.MinioUseSSL,
		Bucket:    cfg.MinioBucket,
	})
	if err != nil {
		if rdb != nil {
			_ = rdb.Close()
		}
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}

	// 4. RabbitMQ
	mq, err := infra.NewRabbitMQ(cfg.RabbitMQURL)
	if err != nil {
		if rdb != nil {
			_ = rdb.Close()
		}
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}

	// 5. migrate
	if err := migrate.AutoMigrate(db); err != nil {
		mq.Close()
		if rdb != nil {
			_ = rdb.Close()
		}
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}

	// 6. 初始化 Snowflake
	idgen.InitSnowflake(cfg.MachineID)

	// 7. 初始化 JWT Secret
	utils.InitJWTSecret(cfg.JWTSecret)

	// 8. 初始化 Bloom Filter
	InitBloomFilter(context.Background(), rdb, db)

	// 9. AI Service Client
	aiClient := infra.NewAIClient(cfg.AIServiceURL)

	// 10. Email Consumer
	emailSender := &email.Sender{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		User:     cfg.SMTPUser,
		Password: cfg.SMTPPassword,
		FromName: cfg.SMTPFromName,
	}
	emailConsumer := consumer.NewEmailConsumer(mq, emailSender)
	if err := emailConsumer.Start(); err != nil {
		mq.Close()
		if rdb != nil {
			_ = rdb.Close()
		}
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}

	return &App{
		Cfg:           cfg,
		DB:            db,
		RDB:           rdb,
		Minio:         minioClient,
		MQ:            mq,
		AI:            aiClient,
		EmailConsumer: emailConsumer,
		H:             wireHandlers(db, rdb, minioClient, mq, aiClient, cfg),
	}, nil
}

func (a *App) Close(ctx context.Context) error {
	// 关闭 mysql
	if a.DB != nil {
		if sqlDB, err := a.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	// 关闭 redis
	if a.RDB != nil {
		_ = a.RDB.Close()
	}
	// 先停 consumer 再关 MQ 连接
	if a.EmailConsumer != nil {
		a.EmailConsumer.Stop()
	}
	if a.MQ != nil {
		a.MQ.Close()
	}
	return nil
}

type Handlers struct {
	AuthHandler *handler.AuthHandler
	UserHandler *handler.UserHandler
	CanvasHandler *handler.CanvasHandler
	FileHandler *handler.FileHandler
	ConversationHandler *handler.ConversationHandler
}

func wireHandlers(db *gorm.DB, rdb *redis.Client, minioClient *minio.Client, mq *infra.RabbitMQ, aiClient *infra.AIClient, cfg *config.Config) *Handlers {
	// Auth
	userRepo := repo.NewUserRepo(db, rdb)
	emailRepo := repo.NewEmailRepo(mq)
	emailSender := &email.Sender{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		User:     cfg.SMTPUser,
		Password: cfg.SMTPPassword,
		FromName: cfg.SMTPFromName,
	}
	authService := service.NewAuthService(userRepo, emailRepo, emailSender)
	authHandler := handler.NewAuthHandler(authService, cfg.CookieSecure)

	// User
	userSerice := service.NewUserService(userRepo)
	userHandler := handler.NewUserHandler(userSerice)

	// Canvas
	canvasRepo := repo.NewCanvasRepo(db, rdb)
	canvasService := service.NewCanvasService(canvasRepo)
	canvasHandler := handler.NewCanvasHandler(canvasService)

	// File
	fileRepo := repo.NewFileRepo(db, rdb, mq, minioClient, cfg.MinioBucket)
	fileService := service.NewFileService(fileRepo)
	fileHandler := handler.NewFileHandler(fileService)

	// Chat
	conversationRepo := repo.NewConversationRepo(db, rdb)
	conversationService := service.NewConversationService(conversationRepo, canvasRepo, fileRepo, aiClient)
	conversationHandler := handler.NewConversationHandler(conversationService)


	return &Handlers{
		AuthHandler: authHandler,
		UserHandler: userHandler,
		CanvasHandler: canvasHandler,
		FileHandler: fileHandler,
		ConversationHandler: conversationHandler,
	}
}

func InitBloomFilter(ctx context.Context, rdb *redis.Client, db *gorm.DB) {
    key := "user_filter"
    err := rdb.BFReserve(ctx, key, 0.01, 10000).Err()
	needSync := false

	if err == nil {
		needSync = true
	} else if strings.Contains(err.Error(), "exists") {
		// 已存在，检查是否需要同步
		log.Println("Bloom filter already exists")
		info, err := rdb.BFInfo(ctx, key).Result()
		if err == nil && info.ItemsInserted == 0 {
			needSync = true
		}
	} else {
		log.Printf("Could not reserve bloom filter: %v", err)
		return
	}
    
	if needSync {
		log.Println("Syncing Bloom Filter with existing users...")
		userRepo := repo.NewUserRepo(db, rdb)
		userRepo.SyncBloomFilter(ctx)
	}
}