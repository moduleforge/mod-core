package httpapi

// Identities shared by the order test suites (apps, entities, field crypto
// keys). The admin/owner may act; the stranger is authenticated but never
// allowed.
const (
	orderAdminID    int64 = 1
	orderOwnerID    int64 = 1
	orderStrangerID int64 = 2
)
