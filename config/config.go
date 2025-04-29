package config

import (
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	Token          string
	DefaultQuota   int
	DefaultEnabled bool
	DatabasePath   string
	UpdateCommands bool
}

func LoadConfig() (*Config, error) {
	pflag.Bool("update_commands", false, "Update commands")
	dev := pflag.Bool("dev", false, "Development mode")
	pflag.Parse()
	viper.BindPFlag("update_commands", pflag.Lookup("update_commands"))
	if *dev {
		viper.SetConfigName("config_dev")
	} else {
		viper.SetConfigName("config")
	}
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
		viper.AddConfigPath("./config")

	// Set defaults
	viper.SetDefault("default_quota", 3)
	viper.SetDefault("default_enabled", false)
	viper.SetDefault("database_path", "bot.db")
	viper.SetDefault("update_commands", false)

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	return &Config{
		Token:          viper.GetString("token"),
		DefaultQuota:   viper.GetInt("default_quota"),
		DefaultEnabled: viper.GetBool("default_enabled"),
		DatabasePath:   viper.GetString("database_path"),
		UpdateCommands: viper.GetBool("update_commands"),
	}, nil
}
