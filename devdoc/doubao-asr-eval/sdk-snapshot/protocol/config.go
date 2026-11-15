package protocol

import (
	"log"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Auth struct {
		AppKey     string `toml:"app_key"`
		AccessKey  string `toml:"access_key"`
		ApiKey     string `toml:"api_key"`
		ResourceID string `toml:"resource_id"`
	}
}

var config Config

func init() {
	if _, err := toml.DecodeFile("../config.toml", &config); err != nil {
		log.Printf("Warning: reading TOML file: %v", err)
	}
}

func AppKey() string {
	if appKey := os.Getenv("APP_KEY"); appKey != "" {
		return appKey
	}
	return config.Auth.AppKey
}

func AccessKey() string {
	if accessKey := os.Getenv("ACCESS_KEY"); accessKey != "" {
		return accessKey
	}
	return config.Auth.AccessKey
}

func ApiKey() string {
	if apiKey := os.Getenv("API_KEY"); apiKey != "" {
		return apiKey
	}
	return config.Auth.ApiKey
}

func ResourceID() string {
	if resourceID := os.Getenv("RESOURCE_ID"); resourceID != "" {
		return resourceID
	}
	if config.Auth.ResourceID != "" {
		return config.Auth.ResourceID
	}
	return "volc.bigasr.sauc.duration"
}
