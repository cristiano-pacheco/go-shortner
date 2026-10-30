package model

import "time"

type Link struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	Code        string `gorm:"not null;uniqueIndex"`
	Destination string `gorm:"not null"`
	Active      bool   `gorm:"not null"`
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

func (Link) TableName() string { return "links" }

// LinkChanges describes fields to update. SetExpiresAt distinguishes clearing
// expiration from leaving it unchanged.
type LinkChanges struct {
	Destination  *string
	Active       *bool
	SetExpiresAt bool
	ExpiresAt    *time.Time
}
