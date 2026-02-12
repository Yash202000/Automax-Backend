package repository

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SettingsRepository interface {
	Get(ctx context.Context) (*models.Settings, error)
	GetOrCreate(ctx context.Context) (*models.Settings, error)
	Update(ctx context.Context, settings *models.Settings) error
}

type settingsRepository struct {
	db *gorm.DB
}

func NewSettingsRepository(db *gorm.DB) SettingsRepository {
	return &settingsRepository{db: db}
}

// Get retrieves the settings (there should only be one record)
func (r *settingsRepository) Get(ctx context.Context) (*models.Settings, error) {
	var settings models.Settings

	err := r.db.WithContext(ctx).First(&settings).Error
	if err != nil {
		return nil, err
	}

	return &settings, nil
}

// GetOrCreate retrieves settings or creates default if none exist
func (r *settingsRepository) GetOrCreate(ctx context.Context) (*models.Settings, error) {
	var settings models.Settings

	err := r.db.WithContext(ctx).First(&settings).Error
	if err == gorm.ErrRecordNotFound {
		// Create default settings
		settings = models.Settings{
			ID:                  uuid.New(),
			AppName:             "Automax",
			AppTagline:          "Streamline your workflow automation",
			AppDescription:      "The complete platform for managing your business operations with powerful automation and analytics.",
			LogoURL:             "/epm-logo.png",
			LogoSmallURL:        "/epm-logo.png",
			FaviconURL:          "/automax-main32.png",
			LogoAltText:         "Automax Logo",
			PrimaryColor:        "#2563eb",
			SecondaryColor:      "#7c3aed",
			AccentColor:         "#059669",
			CopyrightText:       "Automax. All rights reserved.",
			Feature1Title:       "Role-based Access Control",
			Feature1Description: "Granular permission management with hierarchical role assignments",
			Feature2Title:       "Hierarchical Organization",
			Feature2Description: "Manage departments, locations, and classifications in tree structures",
			Feature3Title:       "Real-time Analytics",
			Feature3Description: "Comprehensive dashboards with insights and performance metrics",
			DateFormat:          "YYYY-MM-DD",
			TimeFormat:          "HH:mm:ss",
			DefaultLanguage:     "en",
		}

		if err := r.db.WithContext(ctx).Create(&settings).Error; err != nil {
			return nil, fmt.Errorf("failed to create default settings: %w", err)
		}

		return &settings, nil
	} else if err != nil {
		return nil, err
	}

	return &settings, nil
}

// Update updates the settings
func (r *settingsRepository) Update(ctx context.Context, settings *models.Settings) error {
	return r.db.WithContext(ctx).Save(settings).Error
}
