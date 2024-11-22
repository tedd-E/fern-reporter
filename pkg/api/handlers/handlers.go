package handlers

import (
	"errors"
	"fmt"

	"github.com/guidewire/fern-reporter/config"
	"github.com/guidewire/fern-reporter/pkg/models"
	"github.com/guidewire/fern-reporter/pkg/utils"

	"log"
	"net/http"
	"strconv"
	"time"

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
	var testRunClient models.TestRun
	var testRunDB models.TestRun

	if err := c.ShouldBindJSON(&testRunClient); err != nil {
		fmt.Print(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return // Stop further processing if there is a binding error
	}

	gdb := h.db
	result := gdb.Where("test_seed = ?", testRunClient.TestSeed).First(&testRunDB)

	if result.Error != nil {
		// If it is a new TestRun save to the database directly
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			log.Println("No prior record of the TestRun was found in the database, creating a new entry.")
			saveTestRun(c, gdb, &testRunClient)
			return
		}
		log.Printf("An error occurred when querying for an existing TestRun record: %v", result.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error fetching an existing TestRun record"})
		return
	}
	// If TestRun already exists in the database, append to it
	testRunDB.EndTime = testRunClient.EndTime
	testRunDB.SuiteRuns = append(testRunDB.SuiteRuns, testRunClient.SuiteRuns...)

	saveTestRun(c, gdb, &testRunDB)
}

func saveTestRun(c *gin.Context, gdb *gorm.DB, testRunDB *models.TestRun) {
	// Process tags
	err := ProcessTags(gdb, testRunDB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error processing tags"})
		return // Stop further processing if tag processing fails
	}

	// Save or update the testRun record in the database
	if err := gdb.Save(testRunDB).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error saving record"})
		return // Stop further processing if save fails
	}

	c.JSON(http.StatusCreated, testRunDB)
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

	c.JSON(http.StatusOK, gin.H{
		"testRuns":     testRuns,
		"reportHeader": config.GetHeaderName(),
		"total":        len(testRuns),
	})
}

func (h *Handler) ReportTestRunById(c *gin.Context) {
	var testRun models.TestRun
	id := c.Param("id")
	h.db.Preload("SuiteRuns.SpecRuns").Where("id = ?", id).First(&testRun)

	c.JSON(http.StatusOK, gin.H{
		"reportHeader": config.GetHeaderName(),
		"testRuns":     []models.TestRun{testRun},
	})
}

func (h *Handler) ReportTestRunAllHTML(c *gin.Context) {
	var testRuns []models.TestRun
	h.db.Preload("SuiteRuns.SpecRuns.Tags").Find(&testRuns)
	totalTests, executedTests, passedTests, failedTests := utils.CalculateTestMetrics(testRuns)

	c.HTML(http.StatusOK, "test_runs.html", gin.H{
		"reportHeader":  config.GetHeaderName(),
		"testRuns":      testRuns,
		"totalTests":    totalTests,
		"executedTests": executedTests,
		"passedTests":   passedTests,
		"failedTests":   failedTests,
	})
}

func (h *Handler) ReportTestRunByIdHTML(c *gin.Context) {
	var testRun models.TestRun
	id := c.Param("id")
	h.db.Preload("SuiteRuns.SpecRuns").Where("id = ?", id).First(&testRun)
	testRuns := []models.TestRun{testRun}
	totalTests, executedTests, passedTests, failedTests := utils.CalculateTestMetrics(testRuns)

	c.HTML(http.StatusOK, "test_runs.html", gin.H{
		"reportHeader":  config.GetHeaderName(),
		"testRuns":      []models.TestRun{testRun},
		"totalTests":    totalTests,
		"executedTests": executedTests,
		"passedTests":   passedTests,
		"failedTests":   failedTests,
	})
}

func (h *Handler) ReportTestInsights(c *gin.Context) {
	projectName := c.Param("name")
	startTimeInput := c.Query("startTime")
	endTimeInput := c.Query("endTime")

	startTime, err := ParseTimeFromStringWithDefault(startTimeInput, time.Now().AddDate(-1, 0, 0))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid startTime parameter: %v", err)})
	}
	endTime, err := ParseTimeFromStringWithDefault(endTimeInput, time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid endTimeInput parameter: %v", err)})
	}

	longestTestRuns := GetLongestTestRuns(h, projectName, startTime, endTime)
	numTests := len(longestTestRuns)
	if len(longestTestRuns) > 10 {
		longestTestRuns = longestTestRuns[:10] //only send top 10 longest runs to display
	}

	averageDuration := GetAverageDuration(h, projectName, startTime, endTime)
	fmt.Printf("longestTestRuns: %v\n", longestTestRuns)
	fmt.Printf("averageDuration: %v\n", averageDuration)

	c.HTML(http.StatusOK, "insights.html", gin.H{
		"reportHeader":    config.GetHeaderName(),
		"projectName":     projectName,
		"startTime":       startTime,
		"endTime":         endTime,
		"averageDuration": averageDuration,
		"longestTestRuns": longestTestRuns,
		"numTests":        numTests,
	})
}

func (h *Handler) GetProjectAll(c *gin.Context) {
	var projectNames []string
	h.db.Table("test_runs").
		Distinct("test_project_name").
		Order("test_project_name asc").
		Pluck("test_project_name", &projectNames)
	c.JSON(http.StatusOK, gin.H{
		"projects": projectNames,
	})
}

func (h *Handler) GetTestSummary(c *gin.Context) {
	projectName := c.Param("name")
	testSummaries := GetProjectSpecStatistics(h, projectName)

	c.JSON(http.StatusOK, testSummaries)
}

func (h *Handler) Ping(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "Fern Reporter is running!",
	})
}
