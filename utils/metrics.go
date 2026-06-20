package utils

import (
	"sync"
	"time"
)

type SchedulerMetrics struct {
	LastRunTime time.Time `json:"lastRunTime"`
	LastStatus  string    `json:"lastStatus"` // "success", "failed", "pending"
	ErrorCount  int       `json:"errorCount"`
	RunCount    int       `json:"runCount"`
	LastError   string    `json:"lastError,omitempty"`
}

type SystemMetrics struct {
	mu           sync.RWMutex
	StartTime    time.Time        `json:"startTime"`
	AutoPay      SchedulerMetrics `json:"autoPay"`
	BillReminder SchedulerMetrics `json:"billReminder"`
}

var Metrics = &SystemMetrics{
	StartTime: time.Now(),
	AutoPay: SchedulerMetrics{
		LastStatus: "pending",
	},
	BillReminder: SchedulerMetrics{
		LastStatus: "pending",
	},
}

func (m *SystemMetrics) RecordAutoPayStart() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AutoPay.LastRunTime = time.Now()
	m.AutoPay.RunCount++
}

func (m *SystemMetrics) RecordAutoPaySuccess() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AutoPay.LastStatus = "success"
}

func (m *SystemMetrics) RecordAutoPayFailure(err string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AutoPay.LastStatus = "failed"
	m.AutoPay.ErrorCount++
	m.AutoPay.LastError = err
}

func (m *SystemMetrics) RecordReminderStart() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BillReminder.LastRunTime = time.Now()
	m.BillReminder.RunCount++
}

func (m *SystemMetrics) RecordReminderSuccess() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BillReminder.LastStatus = "success"
}

func (m *SystemMetrics) RecordReminderFailure(err string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BillReminder.LastStatus = "failed"
	m.BillReminder.ErrorCount++
	m.BillReminder.LastError = err
}

func (m *SystemMetrics) GetMetrics() (SchedulerMetrics, SchedulerMetrics, time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.AutoPay, m.BillReminder, m.StartTime
}
