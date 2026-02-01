package app

import (
	"context"

	"github.com/luhao/contextGraph/config"
	"github.com/luhao/contextGraph/internal/infra"
	"github.com/luhao/contextGraph/internal/migrate"
	"github.com/luhao/contextGraph/pkg/idgen"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type App struct {
	Cfg *config.Config
	DB  *gorm.DB
	RDB *redis.Client
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

	// 3. migrate
	if err := migrate.AutoMigrate(db); err != nil {
		if rdb != nil { _ = rdb.Close() }
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}

	// 4. 初始化 Snowflake
	idgen.InitSnowflake(cfg.MachineID)


	return &App{
		Cfg: cfg,
		DB:  db,
		RDB: rdb,
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
	return nil
}