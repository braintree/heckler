package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type PuppetReport struct {
	Host                 string `yaml:"host"`
	Time                 string `yaml:"time"`
	ConfigurationVersion int    `yaml:"configuration_version"`
	TransactionUUID      string `yaml:"transaction_uuid"`
	ReportFormat         int    `yaml:"report_format"`
	PuppetVersion        string `yaml:"puppet_version"`
	Status               string `yaml:"status"`
	TransactionCompleted bool   `yaml:"transaction_completed"`
	Noop                 bool   `yaml:"noop"`
	NoopPending          bool   `yaml:"noop_pending"`
	Environment          string `yaml:"environment"`
	Logs                 []struct {
		Level   string   `yaml:"level"`
		Message string   `yaml:"message"`
		Source  string   `yaml:"source"`
		Tags    []string `yaml:"tags"`
		Time    string   `yaml:"time"`
		File    string   `yaml:"file"`
		Line    int      `yaml:"line"`
	} `yaml:"logs"`
	ResourceStatuses struct {
		FileHomeHathawaySrcHecklerNodes struct {
			Title            string        `yaml:"title"`
			File             string        `yaml:"file"`
			Line             int           `yaml:"line"`
			Resource         string        `yaml:"resource"`
			ResourceType     string        `yaml:"resource_type"`
			ProviderUsed     string        `yaml:"provider_used"`
			ContainmentPath  []string      `yaml:"containment_path"`
			EvaluationTime   float64       `yaml:"evaluation_time"`
			Tags             []string      `yaml:"tags"`
			Time             string        `yaml:"time"`
			Failed           bool          `yaml:"failed"`
			FailedToRestart  bool          `yaml:"failed_to_restart"`
			Changed          bool          `yaml:"changed"`
			OutOfSync        bool          `yaml:"out_of_sync"`
			Skipped          bool          `yaml:"skipped"`
			ChangeCount      int           `yaml:"change_count"`
			OutOfSyncCount   int           `yaml:"out_of_sync_count"`
			Events           []interface{} `yaml:"events"`
			CorrectiveChange bool          `yaml:"corrective_change"`
		} `yaml:"File[/home/hathaway/src/heckler/nodes]"`
		PackageNginx struct {
			Title            string        `yaml:"title"`
			File             string        `yaml:"file"`
			Line             int           `yaml:"line"`
			Resource         string        `yaml:"resource"`
			ResourceType     string        `yaml:"resource_type"`
			ProviderUsed     string        `yaml:"provider_used"`
			ContainmentPath  []string      `yaml:"containment_path"`
			EvaluationTime   float64       `yaml:"evaluation_time"`
			Tags             []string      `yaml:"tags"`
			Time             string        `yaml:"time"`
			Failed           bool          `yaml:"failed"`
			FailedToRestart  bool          `yaml:"failed_to_restart"`
			Changed          bool          `yaml:"changed"`
			OutOfSync        bool          `yaml:"out_of_sync"`
			Skipped          bool          `yaml:"skipped"`
			ChangeCount      int           `yaml:"change_count"`
			OutOfSyncCount   int           `yaml:"out_of_sync_count"`
			Events           []interface{} `yaml:"events"`
			CorrectiveChange bool          `yaml:"corrective_change"`
		} `yaml:"Package[nginx]"`
		FileHomeHathawaySrcHecklerNodesWaldorf struct {
			Title            string        `yaml:"title"`
			File             string        `yaml:"file"`
			Line             int           `yaml:"line"`
			Resource         string        `yaml:"resource"`
			ResourceType     string        `yaml:"resource_type"`
			ProviderUsed     string        `yaml:"provider_used"`
			ContainmentPath  []string      `yaml:"containment_path"`
			EvaluationTime   float64       `yaml:"evaluation_time"`
			Tags             []string      `yaml:"tags"`
			Time             string        `yaml:"time"`
			Failed           bool          `yaml:"failed"`
			FailedToRestart  bool          `yaml:"failed_to_restart"`
			Changed          bool          `yaml:"changed"`
			OutOfSync        bool          `yaml:"out_of_sync"`
			Skipped          bool          `yaml:"skipped"`
			ChangeCount      int           `yaml:"change_count"`
			OutOfSyncCount   int           `yaml:"out_of_sync_count"`
			Events           []interface{} `yaml:"events"`
			CorrectiveChange bool          `yaml:"corrective_change"`
		} `yaml:"File[/home/hathaway/src/heckler/nodes/waldorf]"`
		FileHomeHathawaySrcHecklerNodesWaldorfPoignant struct {
			Title           string   `yaml:"title"`
			File            string   `yaml:"file"`
			Line            int      `yaml:"line"`
			Resource        string   `yaml:"resource"`
			ResourceType    string   `yaml:"resource_type"`
			ProviderUsed    string   `yaml:"provider_used"`
			ContainmentPath []string `yaml:"containment_path"`
			EvaluationTime  float64  `yaml:"evaluation_time"`
			Tags            []string `yaml:"tags"`
			Time            string   `yaml:"time"`
			Failed          bool     `yaml:"failed"`
			FailedToRestart bool     `yaml:"failed_to_restart"`
			Changed         bool     `yaml:"changed"`
			OutOfSync       bool     `yaml:"out_of_sync"`
			Skipped         bool     `yaml:"skipped"`
			ChangeCount     int      `yaml:"change_count"`
			OutOfSyncCount  int      `yaml:"out_of_sync_count"`
			Events          []struct {
				Audited          bool        `yaml:"audited"`
				Property         string      `yaml:"property"`
				PreviousValue    string      `yaml:"previous_value"`
				DesiredValue     string      `yaml:"desired_value"`
				HistoricalValue  interface{} `yaml:"historical_value"`
				Message          string      `yaml:"message"`
				Name             string      `yaml:"name"`
				Status           string      `yaml:"status"`
				Time             string      `yaml:"time"`
				Redacted         interface{} `yaml:"redacted"`
				CorrectiveChange bool        `yaml:"corrective_change"`
			} `yaml:"events"`
			CorrectiveChange bool `yaml:"corrective_change"`
		} `yaml:"File[/home/hathaway/src/heckler/nodes/waldorf/poignant]"`
		ServiceNginx struct {
			Title           string   `yaml:"title"`
			File            string   `yaml:"file"`
			Line            int      `yaml:"line"`
			Resource        string   `yaml:"resource"`
			ResourceType    string   `yaml:"resource_type"`
			ProviderUsed    string   `yaml:"provider_used"`
			ContainmentPath []string `yaml:"containment_path"`
			EvaluationTime  float64  `yaml:"evaluation_time"`
			Tags            []string `yaml:"tags"`
			Time            string   `yaml:"time"`
			Failed          bool     `yaml:"failed"`
			FailedToRestart bool     `yaml:"failed_to_restart"`
			Changed         bool     `yaml:"changed"`
			OutOfSync       bool     `yaml:"out_of_sync"`
			Skipped         bool     `yaml:"skipped"`
			ChangeCount     int      `yaml:"change_count"`
			OutOfSyncCount  int      `yaml:"out_of_sync_count"`
			Events          []struct {
				Audited          bool        `yaml:"audited"`
				Property         string      `yaml:"property"`
				PreviousValue    string      `yaml:"previous_value"`
				DesiredValue     string      `yaml:"desired_value"`
				HistoricalValue  interface{} `yaml:"historical_value"`
				Message          string      `yaml:"message"`
				Name             string      `yaml:"name"`
				Status           string      `yaml:"status"`
				Time             string      `yaml:"time"`
				Redacted         interface{} `yaml:"redacted"`
				CorrectiveChange bool        `yaml:"corrective_change"`
			} `yaml:"events"`
			CorrectiveChange bool `yaml:"corrective_change"`
		} `yaml:"Service[nginx]"`
		SchedulePuppet struct {
			Title            string        `yaml:"title"`
			File             interface{}   `yaml:"file"`
			Line             interface{}   `yaml:"line"`
			Resource         string        `yaml:"resource"`
			ResourceType     string        `yaml:"resource_type"`
			ProviderUsed     interface{}   `yaml:"provider_used"`
			ContainmentPath  []string      `yaml:"containment_path"`
			EvaluationTime   float64       `yaml:"evaluation_time"`
			Tags             []string      `yaml:"tags"`
			Time             string        `yaml:"time"`
			Failed           bool          `yaml:"failed"`
			FailedToRestart  bool          `yaml:"failed_to_restart"`
			Changed          bool          `yaml:"changed"`
			OutOfSync        bool          `yaml:"out_of_sync"`
			Skipped          bool          `yaml:"skipped"`
			ChangeCount      int           `yaml:"change_count"`
			OutOfSyncCount   int           `yaml:"out_of_sync_count"`
			Events           []interface{} `yaml:"events"`
			CorrectiveChange bool          `yaml:"corrective_change"`
		} `yaml:"Schedule[puppet]"`
		ScheduleHourly struct {
			Title            string        `yaml:"title"`
			File             interface{}   `yaml:"file"`
			Line             interface{}   `yaml:"line"`
			Resource         string        `yaml:"resource"`
			ResourceType     string        `yaml:"resource_type"`
			ProviderUsed     interface{}   `yaml:"provider_used"`
			ContainmentPath  []string      `yaml:"containment_path"`
			EvaluationTime   float64       `yaml:"evaluation_time"`
			Tags             []string      `yaml:"tags"`
			Time             string        `yaml:"time"`
			Failed           bool          `yaml:"failed"`
			FailedToRestart  bool          `yaml:"failed_to_restart"`
			Changed          bool          `yaml:"changed"`
			OutOfSync        bool          `yaml:"out_of_sync"`
			Skipped          bool          `yaml:"skipped"`
			ChangeCount      int           `yaml:"change_count"`
			OutOfSyncCount   int           `yaml:"out_of_sync_count"`
			Events           []interface{} `yaml:"events"`
			CorrectiveChange bool          `yaml:"corrective_change"`
		} `yaml:"Schedule[hourly]"`
		ScheduleDaily struct {
			Title            string        `yaml:"title"`
			File             interface{}   `yaml:"file"`
			Line             interface{}   `yaml:"line"`
			Resource         string        `yaml:"resource"`
			ResourceType     string        `yaml:"resource_type"`
			ProviderUsed     interface{}   `yaml:"provider_used"`
			ContainmentPath  []string      `yaml:"containment_path"`
			EvaluationTime   float64       `yaml:"evaluation_time"`
			Tags             []string      `yaml:"tags"`
			Time             string        `yaml:"time"`
			Failed           bool          `yaml:"failed"`
			FailedToRestart  bool          `yaml:"failed_to_restart"`
			Changed          bool          `yaml:"changed"`
			OutOfSync        bool          `yaml:"out_of_sync"`
			Skipped          bool          `yaml:"skipped"`
			ChangeCount      int           `yaml:"change_count"`
			OutOfSyncCount   int           `yaml:"out_of_sync_count"`
			Events           []interface{} `yaml:"events"`
			CorrectiveChange bool          `yaml:"corrective_change"`
		} `yaml:"Schedule[daily]"`
		ScheduleWeekly struct {
			Title            string        `yaml:"title"`
			File             interface{}   `yaml:"file"`
			Line             interface{}   `yaml:"line"`
			Resource         string        `yaml:"resource"`
			ResourceType     string        `yaml:"resource_type"`
			ProviderUsed     interface{}   `yaml:"provider_used"`
			ContainmentPath  []string      `yaml:"containment_path"`
			EvaluationTime   float64       `yaml:"evaluation_time"`
			Tags             []string      `yaml:"tags"`
			Time             string        `yaml:"time"`
			Failed           bool          `yaml:"failed"`
			FailedToRestart  bool          `yaml:"failed_to_restart"`
			Changed          bool          `yaml:"changed"`
			OutOfSync        bool          `yaml:"out_of_sync"`
			Skipped          bool          `yaml:"skipped"`
			ChangeCount      int           `yaml:"change_count"`
			OutOfSyncCount   int           `yaml:"out_of_sync_count"`
			Events           []interface{} `yaml:"events"`
			CorrectiveChange bool          `yaml:"corrective_change"`
		} `yaml:"Schedule[weekly]"`
		ScheduleMonthly struct {
			Title            string        `yaml:"title"`
			File             interface{}   `yaml:"file"`
			Line             interface{}   `yaml:"line"`
			Resource         string        `yaml:"resource"`
			ResourceType     string        `yaml:"resource_type"`
			ProviderUsed     interface{}   `yaml:"provider_used"`
			ContainmentPath  []string      `yaml:"containment_path"`
			EvaluationTime   float64       `yaml:"evaluation_time"`
			Tags             []string      `yaml:"tags"`
			Time             string        `yaml:"time"`
			Failed           bool          `yaml:"failed"`
			FailedToRestart  bool          `yaml:"failed_to_restart"`
			Changed          bool          `yaml:"changed"`
			OutOfSync        bool          `yaml:"out_of_sync"`
			Skipped          bool          `yaml:"skipped"`
			ChangeCount      int           `yaml:"change_count"`
			OutOfSyncCount   int           `yaml:"out_of_sync_count"`
			Events           []interface{} `yaml:"events"`
			CorrectiveChange bool          `yaml:"corrective_change"`
		} `yaml:"Schedule[monthly]"`
		ScheduleNever struct {
			Title            string        `yaml:"title"`
			File             interface{}   `yaml:"file"`
			Line             interface{}   `yaml:"line"`
			Resource         string        `yaml:"resource"`
			ResourceType     string        `yaml:"resource_type"`
			ProviderUsed     interface{}   `yaml:"provider_used"`
			ContainmentPath  []string      `yaml:"containment_path"`
			EvaluationTime   float64       `yaml:"evaluation_time"`
			Tags             []string      `yaml:"tags"`
			Time             string        `yaml:"time"`
			Failed           bool          `yaml:"failed"`
			FailedToRestart  bool          `yaml:"failed_to_restart"`
			Changed          bool          `yaml:"changed"`
			OutOfSync        bool          `yaml:"out_of_sync"`
			Skipped          bool          `yaml:"skipped"`
			ChangeCount      int           `yaml:"change_count"`
			OutOfSyncCount   int           `yaml:"out_of_sync_count"`
			Events           []interface{} `yaml:"events"`
			CorrectiveChange bool          `yaml:"corrective_change"`
		} `yaml:"Schedule[never]"`
		FilebucketPuppet struct {
			Title            string        `yaml:"title"`
			File             interface{}   `yaml:"file"`
			Line             interface{}   `yaml:"line"`
			Resource         string        `yaml:"resource"`
			ResourceType     string        `yaml:"resource_type"`
			ProviderUsed     interface{}   `yaml:"provider_used"`
			ContainmentPath  []string      `yaml:"containment_path"`
			EvaluationTime   float64       `yaml:"evaluation_time"`
			Tags             []string      `yaml:"tags"`
			Time             string        `yaml:"time"`
			Failed           bool          `yaml:"failed"`
			FailedToRestart  bool          `yaml:"failed_to_restart"`
			Changed          bool          `yaml:"changed"`
			OutOfSync        bool          `yaml:"out_of_sync"`
			Skipped          bool          `yaml:"skipped"`
			ChangeCount      int           `yaml:"change_count"`
			OutOfSyncCount   int           `yaml:"out_of_sync_count"`
			Events           []interface{} `yaml:"events"`
			CorrectiveChange bool          `yaml:"corrective_change"`
		} `yaml:"Filebucket[puppet]"`
	} `yaml:"resource_statuses"`
	CorrectiveChange    bool   `yaml:"corrective_change"`
	CatalogUUID         string `yaml:"catalog_uuid"`
	CachedCatalogStatus string `yaml:"cached_catalog_status"`
}

// An example showing how to unmarshal embedded
// structs from YAML.

type StructA struct {
	A string `yaml:"a"`
}

type StructB struct {
	// Embedded structs are not treated as embedded in YAML by default. To do that,
	// add the ",inline" annotation below
	StructA `yaml:",inline"`
	B       string `yaml:"b"`
}

var data = `
a: a string from struct A
b: a string from struct B
`

func main() {
	var pr PuppetReport
	file, err := os.Open("/home/hathaway/src/heckler/reports/commit3_waldorf.yaml")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	data, err := ioutil.ReadAll(file)

	err = yaml.Unmarshal([]byte(data), &pr)
	if err != nil {
		log.Fatalf("cannot unmarshal data: %v", err)
	}
	fmt.Println(pr.Host)
	// for _, log := range pr.Logs {
	// 	fmt.Printf("%v: %v\n", log.Level, log.Source)
	// 	fmt.Println(log.Message)
	// }
}
