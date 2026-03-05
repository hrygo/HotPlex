package storage

import (
	"errors"
	"fmt"
)

// Common storage errors
var (
	ErrNotFound          = errors.New("message not found")
	ErrSessionNotFound   = errors.New("session not found")
	ErrInvalidMessage    = errors.New("invalid message")
	ErrStorageNotEnabled = errors.New("storage not enabled")
	ErrConnectionFailed  = errors.New("database connection failed")
	ErrQueryFailed       = errors.New("query execution failed")
	ErrStoreFailed       = errors.New("message store failed")
	ErrInvalidConfig     = errors.New("invalid storage configuration")
	ErrUnsupportedType   = errors.New("unsupported storage type")
	ErrSessionClosed     = errors.New("storage session closed")
	ErrTransactionFailed = errors.New("transaction failed")
)

// StorageError 存储错误结构
type StorageError struct {
	Code    string
	Message string
	Err     error
}

func (e *StorageError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *StorageError) Unwrap() error {
	return e.Err
}

// NewStorageError 创建新的存储错误
func NewStorageError(code, message string, err error) *StorageError {
	return &StorageError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// IsNotFound 检查是否为未找到错误
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrSessionNotFound)
}

// IsConnectionError 检查是否为连接错误
func IsConnectionError(err error) bool {
	return errors.Is(err, ErrConnectionFailed)
}

// IsConfigError 检查是否为配置错误
func IsConfigError(err error) bool {
	return errors.Is(err, ErrInvalidConfig) || errors.Is(err, ErrUnsupportedType)
}
