package money

import (
	"errors"
	"math/big"
	"strings"
)

const Scale = 8

func Parse(value string) (*big.Rat, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "0"
	}
	result, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, errors.New("invalid decimal")
	}
	return result, nil
}

func Must(value string) *big.Rat {
	result, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return result
}

func Format(value *big.Rat) string { return value.FloatString(Scale) }

func Mul(values ...*big.Rat) *big.Rat {
	result := new(big.Rat).SetInt64(1)
	for _, value := range values {
		result.Mul(result, value)
	}
	return result
}

func Add(values ...*big.Rat) *big.Rat {
	result := new(big.Rat)
	for _, value := range values {
		result.Add(result, value)
	}
	return result
}

func Int(value int) *big.Rat { return new(big.Rat).SetInt64(int64(value)) }
