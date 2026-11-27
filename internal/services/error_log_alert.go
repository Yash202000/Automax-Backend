package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
)

// ErrorLogViewPermission is the permission whose holders receive Critical error alerts.
const ErrorLogViewPermission = "error-logs:view"

var errorModuleNamesAr = map[string]string{
	models.ErrorModuleImport:      "الاستيراد",
	models.ErrorModuleCall:        "المكالمات",
	models.ErrorModuleAttachment:  "المرفقات",
	models.ErrorModuleIncident:    "البلاغات",
	models.ErrorModuleIntegration: "التكامل",
	models.ErrorModuleContact:     "جهات الاتصال",
	models.ErrorModuleAuth:        "تسجيل الدخول / الوصول",
}

type alertGroup struct {
	count int
	first *models.ErrorLog
}

// ErrorLogAlerter sends in-app notifications to admins when a Critical error is logged.
// The first occurrence is sent at once. Repeats of the same error (same fingerprint)
// inside the window are held back, and ONE grouped alert is sent when the window ends.
// Groups are kept in memory, so grouping is per server instance.
type ErrorLogAlerter struct {
	notifier *NotificationService
	userRepo repository.UserRepository
	window   time.Duration

	// deliver sends one alert; replaceable in tests.
	deliver func(e *models.ErrorLog, count int)

	mu     sync.Mutex
	groups map[string]*alertGroup
}

func NewErrorLogAlerter(notifier *NotificationService, userRepo repository.UserRepository, window time.Duration) *ErrorLogAlerter {
	a := &ErrorLogAlerter{notifier: notifier, userRepo: userRepo, window: window, groups: map[string]*alertGroup{}}
	a.deliver = a.send
	return a
}

// Alert handles one saved Critical error.
func (a *ErrorLogAlerter) Alert(e *models.ErrorLog) {
	a.mu.Lock()
	g, exists := a.groups[e.Fingerprint]
	if exists {
		g.count++
		a.mu.Unlock()
		return // repeat inside the window: counted, reported in the grouped alert
	}
	a.groups[e.Fingerprint] = &alertGroup{count: 1, first: e}
	a.mu.Unlock()

	a.deliver(e, 1)
	time.AfterFunc(a.window, func() { a.flush(e.Fingerprint) })
}

// flush ends a window and sends the grouped alert if the error repeated.
func (a *ErrorLogAlerter) flush(fingerprint string) {
	a.mu.Lock()
	g := a.groups[fingerprint]
	delete(a.groups, fingerprint)
	a.mu.Unlock()
	if g != nil && g.count > 1 {
		a.deliver(g.first, g.count)
	}
}

func (a *ErrorLogAlerter) send(e *models.ErrorLog, count int) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ErrorLog] alert recovered: %v", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	users, err := a.userRepo.FindByPermissionCode(ctx, ErrorLogViewPermission)
	if err != nil {
		log.Printf("[ErrorLog] alert: could not load admins: %v", err)
		return
	}
	seen := map[string]bool{}
	var emails []string
	for _, u := range users {
		email := strings.TrimSpace(u.Email)
		if email != "" && !seen[email] {
			seen[email] = true
			emails = append(emails, email)
		}
	}
	if len(emails) == 0 {
		return
	}

	minutes := int(a.window / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	var subject, body, subjectAr, bodyAr string
	if count > 1 {
		subject = fmt.Sprintf("Critical error repeated %d times: %s", count, e.ErrorCode)
		body = fmt.Sprintf("The same Critical error happened %d times in the last %d minutes.\nModule: %s\nAction: %s\nMessage: %s\nFirst Error ID: %s",
			count, minutes, e.Module, e.Action, e.Message, e.ErrorCode)
		subjectAr = fmt.Sprintf("خطأ حرج تكرر %d مرات: %s", count, e.ErrorCode)
		bodyAr = fmt.Sprintf("حدث الخطأ الحرج نفسه %d مرات خلال آخر %d دقيقة.\nالوحدة: %s\nالإجراء: %s\nالرسالة: %s\nرقم أول خطأ: %s",
			count, minutes, errorModuleNamesAr[e.Module], e.Action, e.Message, e.ErrorCode)
	} else {
		subject = fmt.Sprintf("Critical error %s: %s", e.ErrorCode, e.Action)
		body = fmt.Sprintf("A Critical error was recorded.\nModule: %s\nAction: %s\nMessage: %s\nError ID: %s", e.Module, e.Action, e.Message, e.ErrorCode)
		subjectAr = fmt.Sprintf("خطأ حرج %s: %s", e.ErrorCode, e.Action)
		bodyAr = fmt.Sprintf("تم تسجيل خطأ حرج.\nالوحدة: %s\nالإجراء: %s\nالرسالة: %s\nرقم الخطأ: %s",
			errorModuleNamesAr[e.Module], e.Action, e.Message, e.ErrorCode)
	}

	result, err := a.notifier.SendNotification(ctx, "notification", nil, "en", emails, nil, nil, subject, body, nil, nil, nil, nil)
	if err != nil {
		log.Printf("[ErrorLog] alert: send failed: %v", err) // never logged as an error log entry (no loops)
	}
	if result != nil && len(result.InboxLogIDs) > 0 {
		ids := append([]uuid.UUID{}, result.InboxLogIDs...)
		_ = a.notifier.SetArContentOnLogs(ctx, ids, subjectAr, bodyAr)
	}
}
