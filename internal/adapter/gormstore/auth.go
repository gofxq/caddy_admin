package gormstore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
)

const SessionLifetime = 12 * time.Hour

func passwordHash(password string) (string, error) {
	if len(password) < 12 || len(password) > 256 {
		return "", invalid("密码长度必须为 12–256 字节")
	}
	salt := ID()
	key := argon2.IDKey([]byte(password), []byte(salt), 2, 64*1024, 2, 32)
	return salt + ":" + base64.RawStdEncoding.EncodeToString(key), nil
}
func passwordMatches(encoded, password string) bool {
	p := strings.Split(encoded, ":")
	if len(p) != 2 || len(password) > 256 {
		return false
	}
	key := argon2.IDKey([]byte(password), []byte(p[0]), 2, 64*1024, 2, 32)
	want, e := base64.RawStdEncoding.DecodeString(p[1])
	return e == nil && subtle.ConstantTimeCompare(want, key) == 1
}
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Store) Login(ctx context.Context, user, password string) (Session, error) {
	var session Session
	row, e := gorm.G[adminRow](s.db).Where("id = ?", 1).Take(ctx)
	if e != nil && !isMissing(e) {
		return session, e
	}
	hash := row.Password
	session.Username = row.Username
	session.MustChange = row.MustChange
	if hash == "" {
		hash = "dummy:" + base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	}
	ok := passwordMatches(hash, password)
	if !ok || session.Username != user {
		return Session{}, &AppError{Status: 401, Code: "credentials", Message: "用户名或密码错误"}
	}
	return s.issueSession(ctx, session, hash)
}

// Verify the password generation again inside the session-insert transaction.
// A concurrent password reset must not mint a new session from old credentials.
func (s *Store) issueSession(ctx context.Context, session Session, verifiedHash string) (Session, error) {
	session.Token = ID()
	session.CSRF = ID()
	session.Expires = time.Now().Add(SessionLifetime).Unix()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := gorm.G[adminRow](tx).Select("password").Where("id = ?", 1).Take(ctx)
		if err != nil {
			return err
		}
		if current.Password != verifiedHash {
			return &AppError{Status: 401, Code: "credentials", Message: "密码已变化，请重新登录"}
		}
		if err = tx.Where("expires < ?", time.Now().Unix()).Delete(&sessionRow{}).Error; err != nil {
			return err
		}
		return tx.Create(&sessionRow{Hash: tokenHash(session.Token), CSRF: session.CSRF, Expires: session.Expires}).Error
	})
	return session, err
}

func (s *Store) Session(ctx context.Context, token string) (Session, error) {
	var v Session
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := gorm.G[sessionRow](tx).Where("hash = ? AND expires > ?", tokenHash(token), time.Now().Unix()).Take(ctx)
		if err != nil {
			return err
		}
		admin, err := gorm.G[adminRow](tx).Where("id = ?", 1).Take(ctx)
		if err != nil {
			return err
		}
		v.Username, v.MustChange, v.CSRF, v.Expires = admin.Username, admin.MustChange, row.CSRF, row.Expires
		return nil
	})
	if isMissing(err) {
		return v, &AppError{Status: 401, Code: "unauthorized", Message: "请重新登录"}
	}
	if err != nil {
		return v, err
	}
	v.Token = token
	return v, nil
}
func (s *Store) Logout(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("hash = ?", tokenHash(token)).Delete(&sessionRow{}).Error
}
func (s *Store) ChangePassword(ctx context.Context, old, next string) error {
	admin, err := gorm.G[adminRow](s.db).Select("password").Where("id = ?", 1).Take(ctx)
	if err != nil {
		return err
	}
	hash := admin.Password
	if !passwordMatches(hash, old) {
		return &AppError{Status: 401, Code: "credentials", Message: "当前密码错误"}
	}
	return s.writePassword(ctx, next, hash)
}
func (s *Store) ResetPassword(ctx context.Context, password string) error {
	return s.writePassword(ctx, password, "")
}
func (s *Store) writePassword(ctx context.Context, password, expectedHash string) error {
	h, e := passwordHash(password)
	if e != nil {
		return e
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&adminRow{}).Where("id = ?", 1)
		if expectedHash != "" {
			update = update.Where("password = ?", expectedHash)
		}
		result := update.Updates(map[string]any{"password": h, "must_change": false})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			if expectedHash != "" {
				return &AppError{Status: 401, Code: "credentials", Message: "密码已变化，请重新登录"}
			}
			return fmt.Errorf("administrator not initialized")
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&sessionRow{}).Error; err != nil {
			return err
		}
		return auditWith(ctx, tx, auditRow{Time: now(), Actor: "admin", Action: "password.change", Object: "admin", Result: "success"})
	})
}
