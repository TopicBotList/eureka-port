package snippets

import (
	"os"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func CreateZap() *zap.SugaredLogger {
	w := zapcore.AddSync(os.Stdout)

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		w,
		zap.DebugLevel,
	)

	return zap.New(core).Sugar()
}

// Some validators
func ValidatorIsHttpOrHttps(fl validator.FieldLevel) bool {
	// get the field value
	switch fl.Field().Kind() {
	case reflect.String:
		value := fl.Field().String()

		return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
	default:
		return false
	}
}

func ValidatorIsHttps(fl validator.FieldLevel) bool {
	// get the field value
	switch fl.Field().Kind() {
	case reflect.String:
		value := fl.Field().String()

		return strings.HasPrefix(value, "https://")
	default:
		return false
	}
}

func ValidatorNoSpaces(fl validator.FieldLevel) bool {
	// get the field value
	switch fl.Field().Kind() {
	case reflect.String:
		value := fl.Field().String()

		if strings.Contains(value, " ") {
			return false
		}
		return true
	default:
		return false
	}
}
