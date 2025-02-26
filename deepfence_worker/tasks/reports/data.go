package reports

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/deepfence/ThreatMapper/deepfence_server/model"
	"github.com/deepfence/ThreatMapper/deepfence_server/reporters"
	rptScans "github.com/deepfence/ThreatMapper/deepfence_server/reporters/scan"
	rptSearch "github.com/deepfence/ThreatMapper/deepfence_server/reporters/search"
	"github.com/deepfence/ThreatMapper/deepfence_utils/directory"
	"github.com/deepfence/ThreatMapper/deepfence_utils/log"
	sdkUtils "github.com/deepfence/ThreatMapper/deepfence_utils/utils"
	"github.com/deepfence/ThreatMapper/deepfence_worker/utils"
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
)

const (
	VULNERABILITY    = "vulnerability"
	SECRET           = "secret"
	MALWARE          = "malware"
	COMPLIANCE       = "compliance"
	CLOUD_COMPLIANCE = "cloud_compliance"
)

type Info[T any] struct {
	ScanType       string
	Title          string
	StartTime      string
	EndTime        string
	AppliedFilters sdkUtils.ReportFilters
	NodeWiseData   NodeWiseData[T]
}

type ScanData[T any] struct {
	ScanInfo    model.ScanResultsCommon
	ScanResults []T
}

type NodeWiseData[T any] struct {
	SeverityCount map[string]map[string]int32
	ScanData      map[string]ScanData[T]
}

func searchScansFilter(params sdkUtils.ReportParams) rptSearch.SearchScanReq {
	filters := rptSearch.SearchScanReq{}

	filters.NodeFilter = rptSearch.SearchFilter{
		Filters: reporters.FieldsFilters{
			ContainsFilter: reporters.ContainsFilter{
				FieldsValues: map[string][]interface{}{
					"node_type": {params.Filters.NodeType},
				},
			},
		},
	}

	if len(params.Filters.AdvancedReportFilters.HostName) > 0 {
		filters.NodeFilter.Filters.ContainsFilter.FieldsValues["host_name"] = sdkUtils.StringArrayToInterfaceArray(params.Filters.AdvancedReportFilters.HostName)
	}

	if len(params.Filters.AdvancedReportFilters.KubernetesClusterName) > 0 {
		filters.NodeFilter.Filters.ContainsFilter.FieldsValues["kubernetes_cluster_name"] = sdkUtils.StringArrayToInterfaceArray(params.Filters.AdvancedReportFilters.KubernetesClusterName)
	}

	if len(params.Filters.AdvancedReportFilters.PodName) > 0 {
		filters.NodeFilter.Filters.ContainsFilter.FieldsValues["pod_name"] = sdkUtils.StringArrayToInterfaceArray(params.Filters.AdvancedReportFilters.PodName)
	}

	if len(params.Filters.AdvancedReportFilters.ContainerName) > 0 {
		filters.NodeFilter.Filters.ContainsFilter.FieldsValues["node_id"] = sdkUtils.StringArrayToInterfaceArray(params.Filters.AdvancedReportFilters.ContainerName)
	}

	if len(params.Filters.AdvancedReportFilters.ImageName) > 0 {
		filters.NodeFilter.Filters.ContainsFilter.FieldsValues["node_id"] = sdkUtils.StringArrayToInterfaceArray(params.Filters.AdvancedReportFilters.ImageName)
	}

	if len(params.Filters.AdvancedReportFilters.AccountId) > 0 {
		filters.NodeFilter.Filters.ContainsFilter.FieldsValues["account_id"] = sdkUtils.StringArrayToInterfaceArray(params.Filters.AdvancedReportFilters.AccountId)
	}

	if len(params.Filters.ScanId) > 0 {
		filters.ScanFilter = rptSearch.SearchFilter{
			Filters: reporters.FieldsFilters{
				ContainsFilter: reporters.ContainsFilter{
					FieldsValues: map[string][]interface{}{
						"node_id": {params.Filters.ScanId},
					},
				},
			},
		}
	}

	return filters
}

func scanResultFilter(levelKey string, levelValues []string, masked []bool) reporters.FieldsFilters {

	filter := reporters.FieldsFilters{
		MatchFilter: reporters.MatchFilter{
			FieldsValues: map[string][]interface{}{},
		},
		ContainsFilter: reporters.ContainsFilter{
			FieldsValues: map[string][]interface{}{},
		},
	}

	if len(levelValues) > 0 {
		filter.MatchFilter.FieldsValues[levelKey] = sdkUtils.StringArrayToInterfaceArray(levelValues)
	}

	if len(masked) > 0 {
		filter.ContainsFilter.FieldsValues["masked"] = sdkUtils.BoolArrayToInterfaceArray(masked)
	}

	return filter
}

func getVulnerabilityData(ctx context.Context, params sdkUtils.ReportParams) (*Info[model.Vulnerability], error) {

	searchFilter := searchScansFilter(params)

	var (
		end   time.Time = time.Now()
		start time.Time = time.Now()
	)

	if params.Duration > 0 && len(params.Filters.ScanId) == 0 {
		start = end.AddDate(0, 0, -params.Duration)
		searchFilter.ScanFilter = rptSearch.SearchFilter{
			Filters: reporters.FieldsFilters{
				CompareFilters: utils.TimeRangeFilter("updated_at", start, end),
			},
		}
	}

	scans, err := rptSearch.SearchScansReport(ctx, searchFilter, sdkUtils.NEO4J_VULNERABILITY_SCAN)
	if err != nil {
		return nil, err
	}

	log.Info().Msgf("vulnerability scan info: %+v", scans)

	severityFilter := scanResultFilter("cve_severity",
		params.Filters.SeverityOrCheckType, params.Filters.AdvancedReportFilters.Masked)

	nodeWiseData := NodeWiseData[model.Vulnerability]{
		SeverityCount: make(map[string]map[string]int32),
		ScanData:      make(map[string]ScanData[model.Vulnerability]),
	}

	for _, s := range scans {
		result, common, err := rptScans.GetScanResults[model.Vulnerability](
			ctx, sdkUtils.NEO4J_VULNERABILITY_SCAN, s.ScanId, severityFilter, model.FetchWindow{})
		if err != nil {
			log.Error().Err(err).Msgf("failed to get results for %s", s.ScanId)
			continue
		}
		sort.Slice(result[:], func(i, j int) bool {
			return result[i].Cve_severity < result[j].Cve_severity
		})
		nodeWiseData.SeverityCount[s.NodeName] = s.SeverityCounts
		nodeWiseData.ScanData[s.NodeName] = ScanData[model.Vulnerability]{
			ScanInfo:    common,
			ScanResults: result,
		}
	}

	data := Info[model.Vulnerability]{
		ScanType:       VULNERABILITY,
		Title:          "Vulnerability Scan Report",
		StartTime:      start.Format(time.RFC3339),
		EndTime:        end.Format(time.RFC3339),
		AppliedFilters: updateFilters(ctx, params.Filters),
		NodeWiseData:   nodeWiseData,
	}

	return &data, nil
}

func getSecretData(ctx context.Context, params sdkUtils.ReportParams) (*Info[model.Secret], error) {

	searchFilter := searchScansFilter(params)

	var (
		end   time.Time = time.Now()
		start time.Time = time.Now()
	)

	if params.Duration > 0 && len(params.Filters.ScanId) == 0 {
		start = end.AddDate(0, 0, -params.Duration)
		searchFilter.ScanFilter = rptSearch.SearchFilter{
			Filters: reporters.FieldsFilters{
				CompareFilters: utils.TimeRangeFilter("updated_at", start, end),
			},
		}
	}

	scans, err := rptSearch.SearchScansReport(ctx, searchFilter, sdkUtils.NEO4J_SECRET_SCAN)
	if err != nil {
		return nil, err
	}

	log.Info().Msgf("secret scan info: %+v", scans)

	severityFilter := scanResultFilter("level",
		params.Filters.SeverityOrCheckType, params.Filters.AdvancedReportFilters.Masked)

	nodeWiseData := NodeWiseData[model.Secret]{
		SeverityCount: make(map[string]map[string]int32),
		ScanData:      make(map[string]ScanData[model.Secret]),
	}

	for _, s := range scans {
		result, common, err := rptScans.GetScanResults[model.Secret](
			ctx, sdkUtils.NEO4J_SECRET_SCAN, s.ScanId, severityFilter, model.FetchWindow{})
		if err != nil {
			log.Error().Err(err).Msgf("failed to get results for %s", s.ScanId)
			continue
		}
		sort.Slice(result[:], func(i, j int) bool {
			return result[i].Level < result[j].Level
		})
		nodeWiseData.SeverityCount[s.NodeName] = s.SeverityCounts
		nodeWiseData.ScanData[s.NodeName] = ScanData[model.Secret]{
			ScanInfo:    common,
			ScanResults: result,
		}
	}

	data := Info[model.Secret]{
		ScanType:       SECRET,
		Title:          "Secrets Scan Report",
		StartTime:      start.Format(time.RFC3339),
		EndTime:        end.Format(time.RFC3339),
		AppliedFilters: updateFilters(ctx, params.Filters),
		NodeWiseData:   nodeWiseData,
	}

	return &data, nil
}

func getMalwareData(ctx context.Context, params sdkUtils.ReportParams) (*Info[model.Malware], error) {

	searchFilter := searchScansFilter(params)

	var (
		end   time.Time = time.Now()
		start time.Time = time.Now()
	)

	if params.Duration > 0 && len(params.Filters.ScanId) == 0 {
		start = end.AddDate(0, 0, -params.Duration)
		searchFilter.ScanFilter = rptSearch.SearchFilter{
			Filters: reporters.FieldsFilters{
				CompareFilters: utils.TimeRangeFilter("updated_at", start, end),
			},
		}
	}
	scans, err := rptSearch.SearchScansReport(ctx, searchFilter, sdkUtils.NEO4J_MALWARE_SCAN)
	if err != nil {
		return nil, err
	}

	log.Info().Msgf("malware scan info: %+v", scans)

	severityFilter := scanResultFilter("file_severity",
		params.Filters.SeverityOrCheckType, params.Filters.AdvancedReportFilters.Masked)

	nodeWiseData := NodeWiseData[model.Malware]{
		SeverityCount: make(map[string]map[string]int32),
		ScanData:      make(map[string]ScanData[model.Malware]),
	}

	for _, s := range scans {
		result, common, err := rptScans.GetScanResults[model.Malware](
			ctx, sdkUtils.NEO4J_MALWARE_SCAN, s.ScanId, severityFilter, model.FetchWindow{})
		if err != nil {
			log.Error().Err(err).Msgf("failed to get results for %s", s.ScanId)
			continue
		}
		sort.Slice(result[:], func(i, j int) bool {
			return result[i].FileSeverity < result[j].FileSeverity
		})
		nodeWiseData.SeverityCount[s.NodeName] = s.SeverityCounts
		nodeWiseData.ScanData[s.NodeName] = ScanData[model.Malware]{
			ScanInfo:    common,
			ScanResults: result,
		}
	}

	data := Info[model.Malware]{
		ScanType:       MALWARE,
		Title:          "Malware Scan Report",
		StartTime:      start.Format(time.RFC3339),
		EndTime:        end.Format(time.RFC3339),
		AppliedFilters: updateFilters(ctx, params.Filters),
		NodeWiseData:   nodeWiseData,
	}

	return &data, nil
}

func getComplianceData(ctx context.Context, params sdkUtils.ReportParams) (*Info[model.Compliance], error) {

	searchFilter := searchScansFilter(params)

	var (
		end   time.Time = time.Now()
		start time.Time = time.Now()
	)

	if params.Duration > 0 && len(params.Filters.ScanId) == 0 {
		start = end.AddDate(0, 0, -params.Duration)
		searchFilter.ScanFilter = rptSearch.SearchFilter{
			Filters: reporters.FieldsFilters{
				CompareFilters: utils.TimeRangeFilter("updated_at", start, end),
			},
		}
	}
	scans, err := rptSearch.SearchScansReport(ctx, searchFilter, sdkUtils.NEO4J_COMPLIANCE_SCAN)
	if err != nil {
		return nil, err
	}

	log.Info().Msgf("compliance scan info: %+v", scans)

	severityFilter := scanResultFilter("compliance_check_type",
		params.Filters.SeverityOrCheckType, params.Filters.AdvancedReportFilters.Masked)

	nodeWiseData := NodeWiseData[model.Compliance]{
		SeverityCount: make(map[string]map[string]int32),
		ScanData:      make(map[string]ScanData[model.Compliance]),
	}

	for _, s := range scans {
		result, common, err := rptScans.GetScanResults[model.Compliance](
			ctx, sdkUtils.NEO4J_COMPLIANCE_SCAN, s.ScanId, severityFilter, model.FetchWindow{})
		if err != nil {
			log.Error().Err(err).Msgf("failed to get results for %s", s.ScanId)
			continue
		}
		sort.Slice(result[:], func(i, j int) bool {
			return result[i].ComplianceCheckType < result[j].ComplianceCheckType
		})
		nodeWiseData.SeverityCount[s.NodeName] = s.SeverityCounts
		nodeWiseData.ScanData[s.NodeName] = ScanData[model.Compliance]{
			ScanInfo:    common,
			ScanResults: result,
		}
	}

	data := Info[model.Compliance]{
		ScanType:       COMPLIANCE,
		Title:          "Compliance Scan Report",
		StartTime:      start.Format(time.RFC3339),
		EndTime:        end.Format(time.RFC3339),
		AppliedFilters: updateFilters(ctx, params.Filters),
		NodeWiseData:   nodeWiseData,
	}

	return &data, nil
}

func getCloudComplianceData(ctx context.Context, params sdkUtils.ReportParams) (*Info[model.CloudCompliance], error) {

	searchFilter := searchScansFilter(params)

	var (
		end   time.Time = time.Now()
		start time.Time = time.Now()
	)

	if params.Duration > 0 && len(params.Filters.ScanId) == 0 {
		start = end.AddDate(0, 0, -params.Duration)
		searchFilter.ScanFilter = rptSearch.SearchFilter{
			Filters: reporters.FieldsFilters{
				CompareFilters: utils.TimeRangeFilter("updated_at", start, end),
			},
		}
	}

	scans, err := rptSearch.SearchScansReport(ctx, searchFilter, sdkUtils.NEO4J_CLOUD_COMPLIANCE_SCAN)
	if err != nil {
		return nil, err
	}

	log.Info().Msgf("cloud compliance scan info: %+v", scans)

	severityFilter := scanResultFilter("compliance_check_type",
		params.Filters.SeverityOrCheckType, params.Filters.AdvancedReportFilters.Masked)

	nodeWiseData := NodeWiseData[model.CloudCompliance]{
		SeverityCount: make(map[string]map[string]int32),
		ScanData:      make(map[string]ScanData[model.CloudCompliance]),
	}

	for _, s := range scans {
		result, common, err := rptScans.GetScanResults[model.CloudCompliance](
			ctx, sdkUtils.NEO4J_CLOUD_COMPLIANCE_SCAN, s.ScanId, severityFilter, model.FetchWindow{})
		if err != nil {
			log.Error().Err(err).Msgf("failed to get results for %s", s.ScanId)
			continue
		}
		sort.Slice(result[:], func(i, j int) bool {
			return result[i].ComplianceCheckType < result[j].ComplianceCheckType
		})
		nodeWiseData.SeverityCount[s.NodeName] = s.SeverityCounts
		nodeWiseData.ScanData[s.NodeName] = ScanData[model.CloudCompliance]{
			ScanInfo:    common,
			ScanResults: result,
		}
	}

	data := Info[model.CloudCompliance]{
		ScanType:       CLOUD_COMPLIANCE,
		Title:          "Cloud Compliance Scan Report",
		StartTime:      start.Format(time.RFC3339),
		EndTime:        end.Format(time.RFC3339),
		AppliedFilters: updateFilters(ctx, params.Filters),
		NodeWiseData:   nodeWiseData,
	}

	return &data, nil
}

func updateFilters(ctx context.Context, original sdkUtils.ReportFilters) sdkUtils.ReportFilters {
	if len(original.AdvancedReportFilters.ImageName) > 0 {
		original.AdvancedReportFilters.ImageName = NodeIdToNodeName(ctx, original.AdvancedReportFilters.ImageName, "container_image")
	}
	if len(original.AdvancedReportFilters.ContainerName) > 0 {
		original.AdvancedReportFilters.ContainerName = NodeIdToNodeName(ctx, original.AdvancedReportFilters.ContainerName, "container")
	}
	return original
}

func NodeIdToNodeName(ctx context.Context, nodeIds []string, node_type string) []string {
	nodes := []string{}

	driver, err := directory.Neo4jClient(ctx)
	if err != nil {
		log.Error().Msg(err.Error())
		return nodes
	}

	session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	if err != nil {
		log.Error().Msg(err.Error())
		return nodes
	}
	defer session.Close()

	tx, err := session.BeginTransaction(neo4j.WithTxTimeout(30 * time.Second))
	if err != nil {
		log.Error().Msg(err.Error())
		return nodes
	}
	defer tx.Close()

	query := ""
	nodeStr := strings.Join(nodeIds, "\", \"")

	switch node_type {
	case "container_image":
		query = `
		MATCH (n:ContainerImage)
		WHERE n.node_id in ["%s"]
		RETURN n.docker_image_name + ':' + n.docker_image_tag as name
		`
	case "container":
		query = `
		MATCH (n:Container)
		WHERE n.node_id in ["%s"]
		RETURN n.node_name as name
		`
	}

	result, err := tx.Run(fmt.Sprintf(query, nodeStr), map[string]interface{}{})
	if err != nil {
		log.Error().Msg(err.Error())
		return nodes
	}

	records, err := result.Collect()
	if err != nil {
		log.Error().Msg(err.Error())
		return nodes
	}

	for _, rec := range records {
		name, ok := rec.Get("name")
		if !ok {
			continue
		}
		nodes = append(nodes, name.(string))
	}

	return nodes
}
