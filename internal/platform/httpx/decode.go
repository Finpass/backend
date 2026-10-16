package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"shenzen/backend/internal/shared/apperr"
)

const maxBodyBytes = 1 << 20

var validate = func() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		return name
	})
	return v
}()

func Decode[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var dst T
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&dst); err != nil {
		return dst, apperr.Wrap(apperr.BadRequest, "Invalid JSON body", err)
	}
	if err := validate.Struct(dst); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			details := make([]apperr.Detail, len(ve))
			for i, fe := range ve {
				rule := fe.Tag()
				if fe.Param() != "" {
					rule += "=" + fe.Param()
				}
				details[i] = apperr.Detail{Field: fe.Namespace()[strings.Index(fe.Namespace(), ".")+1:], Message: rule}
			}
			return dst, apperr.Validation(details...)
		}
		return dst, apperr.Wrap(apperr.Internal, "Validation failed unexpectedly", err)
	}
	return dst, nil
}
