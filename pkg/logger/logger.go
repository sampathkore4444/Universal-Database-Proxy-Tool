package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logger *zap.Logger
	sugar  *zap.SugaredLogger
)

func Init(debug bool) error {
	var config zap.Config
	if debug {
		config = zap.NewDevelopmentConfig()
	} else {
		config = zap.NewProductionConfig()
	}

	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	l, err := config.Build()
	if err != nil {
		return err
	}

	logger = l
	sugar = l.Sugar()

	return nil
}

func GetLogger() *zap.Logger {
	if logger == nil {
		_ = Init(false)
	}
	return logger
}

func GetSugar() *zap.SugaredLogger {
	if sugar == nil {
		_ = Init(false)
	}
	return sugar
}

func WithFields(fields ...zap.Field) *zap.Logger {
	return GetLogger().With(fields...)
}

func Debug(msg string, fields ...zap.Field) {
	GetLogger().Debug(msg, fields...)
}

func Info(msg string, fields ...zap.Field) {
	GetLogger().Info(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	GetLogger().Warn(msg, fields...)
}

func Error(msg string, fields ...zap.Field) {
	GetLogger().Error(msg, fields...)
}

func Fatal(msg string, fields ...zap.Field) {
	GetLogger().Fatal(msg, fields...)
	os.Exit(1)
}

func Sync() {
	if logger != nil {
		_ = logger.Sync()
	}
}
