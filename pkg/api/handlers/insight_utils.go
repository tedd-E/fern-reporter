package handlers

import (
	"github.com/guidewire/fern-reporter/pkg/models"
	"time"
)

func GetLongestTestRuns(h *Handler, projectName string, window time.Time) []models.TestRunInsight {
	var testRuns []models.TestRunInsight

	h.db.Table("test_runs").
		Joins("INNER JOIN suite_runs ON test_runs.id = suite_runs.test_run_id").
		Joins("INNER JOIN spec_runs ON suite_runs.id = spec_runs.suite_id").
		Select("*, (EXTRACT(EPOCH FROM end_time) - EXTRACT(EPOCH FROM start_time)) AS duration").
		Select("suite_runs.id, test_runs.test_project_name, test_runs.start_time, test_runs.end_time,"+
			"ROUND(AVG(CASE WHEN spec_runs.status = 'passed' THEN 100.0 ELSE 0.0 END), 3) AS pass_rate, "+
			"(test_runs.end_time - test_runs.start_time) AS duration").
		Where("test_runs.start_time >= ?", window).
		Where("test_project_name = ?", projectName).
		Group("suite_runs.id, test_runs.test_project_name, test_runs.start_time, test_runs.end_time").
		Order("duration DESC").
		Find(&testRuns)

	return testRuns
}

func GetAverageDuration(h *Handler, projectName string, window time.Time) float64 {
	var averageDuration float64
	h.db.Table("test_runs").
		Select("AVG(EXTRACT(EPOCH FROM (end_time - start_time)))").
		Where("test_project_name = ?", projectName).
		Where("start_time >= ?", window).
		Scan(&averageDuration)
	return averageDuration
}
