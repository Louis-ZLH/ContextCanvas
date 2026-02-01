package config

import (
	"os"
	"strconv"
)

type Config struct {
	Env string

	HTTPAddr string

	MySQLDSN string

	RedisAddr     string
	RedisPassword string
	RedisDB       int
	MachineID     int64
}

func Load() *Config {
	// 你也可以换成 viper / envconfig，这里用最朴素的方式

	machine_id , err := strconv.ParseInt(getEnv("MACHINE_ID", "0"), 10, 64)
	if err != nil {
		panic(err)
	}


	cfg := &Config{
		Env:      getEnv("APP_ENV", "dev"),
		HTTPAddr: getEnv("HTTP_ADDR", ":8080"),

		MySQLDSN: getEnv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3306)/context_graph?charset=utf8mb4&parseTime=True&loc=Local"),

		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		MachineID:     machine_id,
	}

	cfg.RedisDB = 0
	return cfg
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
