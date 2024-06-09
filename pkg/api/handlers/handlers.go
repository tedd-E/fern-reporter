package handlers

import (
	"errors"
	"fmt"
	"github.com/guidewire/fern-reporter/config"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/guidewire/fern-reporter/pkg/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	db *gorm.DB
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{db: db}
}

func (h *Handler) CreateTestRun(c *gin.Context) {
	var testRun models.TestRun

	if err := c.ShouldBindJSON(&testRun); err != nil {
		fmt.Print(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return // Stop further processing if there is a binding error
	}

	gdb := h.db
	isNewRecord := testRun.ID == 0

	// If it's not a new record, try to find it first
	if !isNewRecord {
		if err := gdb.Where("id = ?", testRun.ID).First(&models.TestRun{}).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
			return // Stop further processing if record not found
		}
	}

	// Process tags
	err := ProcessTags(gdb, &testRun)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error processing tags"})
		return // Stop further processing if tag processing fails
	}

	// Save or update the testRun record in the database
	if err := gdb.Save(&testRun).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error saving record"})
		return // Stop further processing if save fails
	}

	c.JSON(http.StatusCreated, &testRun)
}

func ProcessTags(db *gorm.DB, testRun *models.TestRun) error {
	for i, suite := range testRun.SuiteRuns {
		for j, spec := range suite.SpecRuns {
			var processedTags []models.Tag
			for _, tag := range spec.Tags {
				var existingTag models.Tag

				// Check if the tag already exists
				result := db.Where("name = ?", tag.Name).First(&existingTag)

				if errors.Is(result.Error, gorm.ErrRecordNotFound) {
					// If the tag does not exist, create a new one
					newTag := models.Tag{Name: tag.Name}
					if err := db.Create(&newTag).Error; err != nil {
						return err // Return error if tag creation fails
					}
					processedTags = append(processedTags, newTag)
				} else if result.Error != nil {
					// Return error if there is a problem fetching the tag
					return result.Error
				} else {
					// If the tag exists, use the existing tag
					processedTags = append(processedTags, existingTag)
				}
			}
			// Correctly associate the processed tags with the specific spec run
			testRun.SuiteRuns[i].SpecRuns[j].Tags = processedTags
		}
	}
	return nil
}

func (h *Handler) GetTestRunAll(c *gin.Context) {
	var testRuns []models.TestRun
	h.db.Find(&testRuns)
	c.JSON(http.StatusOK, testRuns)
}

func (h *Handler) GetTestRunByID(c *gin.Context) {
	var testRun models.TestRun
	id := c.Param("id")
	h.db.Where("id = ?", id).First(&testRun)
	c.JSON(http.StatusOK, testRun)

}

func (h *Handler) UpdateTestRun(c *gin.Context) {
	var testRun models.TestRun
	id := c.Param("id")

	db := h.db
	if err := db.Where("id = ?", id).First(&testRun).Error; err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	err := c.BindJSON(&testRun)
	if err != nil {
		log.Fatalf("error binding json: %v", err)
	}

	db.Save(&testRun)
	c.JSON(http.StatusOK, &testRun)
}

func (h *Handler) DeleteTestRun(c *gin.Context) {
	var testRun models.TestRun
	id := c.Param("id")
	if testRunID, err := strconv.Atoi(id); err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	} else {
		testRun.ID = uint64(testRunID)
	}

	result := h.db.Delete(&testRun)
	if result.Error != nil {
		// If there was an error during the delete operation
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error deleting test run"})
		return
	} else if result.RowsAffected == 0 {
		// If no rows were affected, it means no record was found with the provided ID
		c.JSON(http.StatusNotFound, gin.H{"error": "test run not found"})
		return
	}

	c.JSON(http.StatusOK, &testRun)
}

func (h *Handler) ReportTestRunAll(c *gin.Context) {
	var testRuns []models.TestRun
	h.db.Preload("SuiteRuns.SpecRuns.Tags").Find(&testRuns)
	c.HTML(http.StatusOK, "test_runs.html", gin.H{
		"reportHeader": config.GetHeaderName(),
		"testRuns":     testRuns,
	})
}

func (h *Handler) ReportTestRunById(c *gin.Context) {
	var testRun models.TestRun
	id := c.Param("id")
	h.db.Preload("SuiteRuns.SpecRuns").Where("id = ?", id).First(&testRun)
	c.HTML(http.StatusOK, "test_runs.html", gin.H{
		"reportHeader": config.GetHeaderName(),
		"testRuns":     []models.TestRun{testRun},
	})
}

//TODO: add functions here that calculate insights with the query output in testRuns
/*
	1. Average sliding window time for tests:
	    a. include average for each individual test, as well as all tests in a project
	2. most time consuming tests top 10
	3. total tests run over the period of time
	4. success rate of each test over the window of time
	    a. tests that fail the most often
	5. success rate of the entire suite over the window of time
*/

func (h *Handler) ReportTestInsights(c *gin.Context) {
	var projectName string
	id := c.Param("id")
	h.db.Model(&models.TestRun{}).Where("id = ?", id).Pluck("test_project_name", &projectName) //extract project name corresponding to ID

	//h.db.Preload("TestRuns").Where("test_project_name = ?", projectName).Where("start_time >= ?", window).Find(&testRuns)

	c.HTML(http.StatusOK, "insights.html", gin.H{
		"reportHeader":    config.GetHeaderName(),
		"projectName":     projectName,
		"averageDuration": GetAverageDuration(h, projectName),
		"testRuns":        GetLongestTestRuns(h, projectName),
	})
}

// returns longest specs
func GetLongestTestRuns(h *Handler, projectName string) []models.TestRun {
	var testRuns []models.TestRun
	//h.db.Table("test_runs").
	h.db.Preload("SuiteRuns.SpecRuns").
		Select("*, (EXTRACT(EPOCH FROM end_time) - EXTRACT(EPOCH FROM start_time)) AS duration").
		//Select("ROUND(AVG(CASE WHEN status = 'passed' THEN 100.0 ELSE 0.0 END), 3) AS spec_pass_rate").
		Where("test_project_name = ?", projectName).
		Order("duration DESC").
		Limit(10).
		Find(&testRuns)

	return testRuns
}

func GetAverageDuration(h *Handler, projectName string) float64 {
	window := time.Now().AddDate(0, 0, -30) //TODO: parameterize
	var averageDuration float64
	h.db.Table("test_runs").
		Select("AVG(EXTRACT(EPOCH FROM (end_time - start_time)))").
		Where("test_project_name = ?", projectName).
		Where("start_time >= ?", window).
		Scan(&averageDuration)
	return averageDuration
}

func (h *Handler) Ping(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "Fern Reporter is running!",
	})
}
