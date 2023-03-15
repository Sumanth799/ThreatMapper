package model

import (
	"context"
	"fmt"

	"github.com/deepfence/golang_deepfence_sdk/utils/directory"
	"github.com/deepfence/golang_deepfence_sdk/utils/log"
	"github.com/deepfence/golang_deepfence_sdk/utils/utils"
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
)

const PostureProviderAWS = "aws"
const PostureProviderAWSOrg = "aws_org"
const PostureProviderGCP = "gcp"
const PostureProviderAzure = "azure"
const PostureProviderLinux = "linux"
const PostureProviderKubernetes = "kubernetes"

var SupportedPostureProviders = []string{PostureProviderAWS, PostureProviderAWSOrg, PostureProviderGCP,
	PostureProviderAzure, PostureProviderLinux, PostureProviderKubernetes}

type CloudNodeAccountRegisterReq struct {
	NodeId              string            `json:"node_id" required:"true"`
	CloudAccount        string            `json:"cloud_account" required:"true"`
	CloudProvider       string            `json:"cloud_provider" required:"true"  enum:"aws,gcp,azure"`
	MonitoredAccountIds map[string]string `json:"monitored_account_ids"`
	OrgAccountId        string            `json:"org_acc_id"`
}

type CloudNodeAccountRegisterResp struct {
	Data CloudNodeAccountRegisterRespData `json:"data"`
}

type CloudNodeAccountRegisterRespData struct {
	Scans            map[string]CloudComplianceScanDetails `json:"scans"`
	CloudtrailTrails []CloudNodeCloudtrailTrail            `json:"cloudtrail_trails"`
	Refresh          string                                `json:"refresh"`
}

type CloudNodeAccountsListReq struct {
	CloudProvider string      `json:"cloud_provider"`
	Window        FetchWindow `json:"window" required:"true"`
}

type CloudNodeProvidersListReq struct{}

type CloudNodeProvidersListResp struct {
	Providers []PostureProvider `json:"providers" required:"true"`
}

type CloudNodeAccountsListResp struct {
	CloudNodeAccountInfo []CloudNodeAccountInfo `json:"cloud_node_accounts_info" required:"true"`
	Total                int                    `json:"total" required:"true"`
}

type CloudNodeAccountInfo struct {
	NodeId               string `json:"node_id"`
	NodeName             string `json:"node_name"`
	CloudProvider        string `json:"cloud_provider"`
	CompliancePercentage string `json:"compliance_percentage"`
	Active               string `json:"active"`
}

type CloudComplianceScanDetails struct {
	ScanId    string   `json:"scan_id"`
	ScanType  string   `json:"scan_type"`
	AccountId string   `json:"account_id"`
	Controls  []string `json:"controls"`
}

type CloudNodeCloudtrailTrail struct {
	AccountId string `json:"account_id"`
	TrailName string `json:"trail_name"`
}

type PendingCloudComplianceScan struct {
	ScanId    string   `json:"scan_id"`
	ScanType  string   `json:"scan_type"`
	Controls  []string `json:"controls"`
	AccountId string   `json:"account_id"`
}

type CloudNodeControlReq struct {
	NodeId         string `json:"node_id"`
	CloudProvider  string `json:"cloud_provider" required:"true" enum:"aws,gcp,azure"`
	ComplianceType string `json:"compliance_type" required:"true"`
}

type CloudNodeEnableDisableReq struct {
	NodeId      string   `json:"node_id"`
	ControlsIds []string `json:"control_ids"`
}

type CloudNodeControlResp struct {
	Controls []CloudNodeComplianceControl `json:"controls"`
}

type CloudNodeComplianceControl struct {
	ControlId         string   `json:"control_id"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Service           string   `json:"service"`
	CategoryHierarchy []string `json:"category_hierarchy"`
	Enabled           bool     `json:"enabled"`
}

type PostureProvider struct {
	Name                 string  `json:"name"`
	NodeCount            int     `json:"node_count"`
	NodeLabel            string  `json:"node_label"`
	ScanCount            int     `json:"scan_count"`
	CompliancePercentage float64 `json:"compliance_percentage"`
	ResourceCount        int     `json:"resource_count"`
}

func UpsertCloudComplianceNode(ctx context.Context, nodeDetails map[string]interface{}, parentNodeId string) error {
	driver, err := directory.Neo4jClient(ctx)
	if err != nil {
		return err
	}

	session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close()

	tx, err := session.BeginTransaction()
	if err != nil {
		return err
	}
	defer tx.Close()

	matchNodeRes, err := tx.Run(`
		WITH $param as row
		MATCH (n:Node{node_id:row.node_id})
		SET n+= row, n.updated_at = TIMESTAMP()`,
		map[string]interface{}{
			"param": nodeDetails,
		})
	if err != nil {
		return err
	}
	_, err = matchNodeRes.Single()
	if err != nil {
		if parentNodeId == "" {
			if _, err := tx.Run(`
			WITH $param as row
			MERGE (n:Node{node_id:row.node_id})
			SET n+= row, n.updated_at = TIMESTAMP()`,
				map[string]interface{}{
					"param": nodeDetails,
				}); err != nil {
				return err
			}
		} else {
			if _, err := tx.Run(`
			MATCH (m:Node{node_id: $parent_node_id})
			WITH $param as row, m
			MERGE (n:Node{node_id:row.node_id}) <-[:IS_CHILD]- (m)
			SET n+= row, n.updated_at = TIMESTAMP()`,
				map[string]interface{}{
					"param":          nodeDetails,
					"parent_node_id": parentNodeId,
				}); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func GetCloudProvidersList(ctx context.Context) ([]PostureProvider, error) {
	var postureProviders []PostureProvider
	driver, err := directory.Neo4jClient(ctx)
	if err != nil {
		return postureProviders, err
	}

	session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	if err != nil {
		return postureProviders, err
	}
	defer session.Close()

	tx, err := session.BeginTransaction()
	if err != nil {
		return postureProviders, err
	}
	defer tx.Close()

	for _, postureProviderName := range SupportedPostureProviders {
		postureProvider := PostureProvider{
			Name:                 postureProviderName,
			NodeCount:            0,
			ScanCount:            0,
			CompliancePercentage: 0,
			ResourceCount:        0,
		}
		scanType := utils.NEO4J_CLOUD_COMPLIANCE_SCAN
		neo4jNodeType := "Node"
		nodeLabel := "Hosts"
		if postureProviderName == PostureProviderKubernetes {
			neo4jNodeType = "KubernetesCluster"
			nodeLabel = "Clusters"
		}
		if postureProviderName == PostureProviderLinux || postureProviderName == PostureProviderKubernetes {
			postureProvider.NodeLabel = nodeLabel
			scanType = utils.NEO4J_COMPLIANCE_SCAN
			nodeRes, err := tx.Run(fmt.Sprintf(`
			MATCH (m:%s)
			WITH COUNT(DISTINCT m.node_id) AS account_count
			MATCH (n:%s)-[:SCANNED]->(m:%s)
			WITH account_count, COUNT(DISTINCT n.node_id) AS scan_count
			MATCH (m:%s)<-[:SCANNED]-(n:%s)-[:DETECTED]->(c:Compliance)
			WITH account_count, scan_count, COUNT(c) AS total_compliance_count
			MATCH (m:%s)<-[:SCANNED]-(n:%s)-[:DETECTED]->(c1:Compliance)
			WHERE c1.status IN ['ok', 'info', 'skip']
			RETURN account_count, scan_count, CASE WHEN total_compliance_count = 0 THEN 0.0 ELSE COUNT(c1.status)*100.0/total_compliance_count END AS compliance_percentage`,
				neo4jNodeType, scanType, neo4jNodeType, neo4jNodeType, scanType, neo4jNodeType, scanType),
				map[string]interface{}{})
			nodeRec, err := nodeRes.Single()
			if err != nil {
				log.Error().Msgf("Provider query error for %s: %v", postureProviderName, err)
			} else {
				postureProvider.NodeCount = int(nodeRec.Values[0].(int64))
				postureProvider.ScanCount = int(nodeRec.Values[1].(int64))
				postureProvider.CompliancePercentage = nodeRec.Values[2].(float64)
			}
		} else if postureProviderName == PostureProviderAWSOrg {
			cloudProvider := PostureProviderAWS
			postureProvider.NodeLabel = "Organizations"
			nodeRes, err := tx.Run(fmt.Sprintf(`
			MATCH (o:%s{cloud_provider:$cloud_provider+'_org'})
			WITH COUNT(DISTINCT o.node_id) AS account_count
			OPTIONAL MATCH (p:CloudResource)
			WHERE p.cloud_provider = $cloud_provider
			WITH account_count, COUNT(*) AS resource_count
			OPTIONAL MATCH (n:%s)-[:SCANNED]->(m:%s{cloud_provider: $cloud_provider})<-[:IS_CHILD]-(o:%s{cloud_provider:$cloud_provider+'_org'})
			WITH account_count, resource_count, COUNT(DISTINCT n.node_id) AS scan_count
			OPTIONAL MATCH (m:%s{cloud_provider: $cloud_provider})<-[:SCANNED]-(n:%s)-[:DETECTED]->(c:CloudComplianceResult), (o:%s{cloud_provider:$cloud_provider+'_org'}) -[:IS_CHILD]-> (m:%s{cloud_provider: $cloud_provider})
			WITH account_count, resource_count, scan_count, COUNT(c) AS total_compliance_count
			OPTIONAL MATCH (m:%s{cloud_provider: $cloud_provider})<-[:SCANNED]-(n:%s)-[:DETECTED]->(c1:CloudComplianceResult), (o:%s{cloud_provider:$cloud_provider+'_org'}) -[:IS_CHILD]-> (m:%s{cloud_provider: $cloud_provider})
			WHERE c1.status IN ['ok', 'info', 'skip']
			RETURN account_count, resource_count, scan_count,
				CASE WHEN total_compliance_count = 0 THEN 0.0 ELSE COUNT(c1.status)*100.0/total_compliance_count END AS compliance_percentage`, neo4jNodeType, scanType,
				neo4jNodeType, neo4jNodeType, neo4jNodeType, scanType, neo4jNodeType, neo4jNodeType, neo4jNodeType,
				scanType, neo4jNodeType, neo4jNodeType),
				map[string]interface{}{
					"cloud_provider": cloudProvider,
				})
			nodeRec, err := nodeRes.Single()
			if err != nil {
				log.Error().Msgf("Provider query error for %s: %v", postureProviderName, err)
			} else {
				postureProvider.NodeCount = int(nodeRec.Values[0].(int64))
				postureProvider.ResourceCount = int(nodeRec.Values[1].(int64))
				postureProvider.ScanCount = int(nodeRec.Values[2].(int64))
				postureProvider.CompliancePercentage = nodeRec.Values[3].(float64)
			}
		} else {
			postureProvider.NodeLabel = "Accounts"
			nodeRes, err := tx.Run(fmt.Sprintf(`
			MATCH (m:%s{cloud_provider: $cloud_provider})
			WITH COUNT(DISTINCT m.node_id) AS account_count
			OPTIONAL MATCH (p:CloudResource)
			WHERE p.cloud_provider = $cloud_provider
			WITH account_count, COUNT(*) AS resource_count
			OPTIONAL MATCH (n:%s)-[:SCANNED]->(m:%s{cloud_provider: $cloud_provider})
			WITH account_count, resource_count, COUNT(DISTINCT n.node_id) AS scan_count
			OPTIONAL MATCH (m:%s{cloud_provider: $cloud_provider})<-[:SCANNED]-(n:%s)-[:DETECTED]->(c:CloudComplianceResult)
			WITH account_count, resource_count, scan_count, COUNT(c) AS total_compliance_count
			OPTIONAL MATCH (m:%s{cloud_provider: $cloud_provider})<-[:SCANNED]-(n:%s)-[:DETECTED]->(c1:CloudComplianceResult)
			WHERE c1.status IN ['ok', 'info', 'skip']
			RETURN account_count, resource_count, scan_count,
				CASE WHEN total_compliance_count = 0 THEN 0.0 ELSE COUNT(c1.status)*100.0/total_compliance_count END AS compliance_percentage`, neo4jNodeType, scanType,
				neo4jNodeType, neo4jNodeType, scanType, neo4jNodeType, scanType),
				map[string]interface{}{
					"cloud_provider": postureProviderName,
				})
			nodeRec, err := nodeRes.Single()
			if err != nil {
				log.Error().Msgf("Provider query error for %s: %v", postureProviderName, err)
			} else {
				postureProvider.NodeCount = int(nodeRec.Values[0].(int64))
				postureProvider.ResourceCount = int(nodeRec.Values[1].(int64))
				postureProvider.ScanCount = int(nodeRec.Values[2].(int64))
				postureProvider.CompliancePercentage = nodeRec.Values[3].(float64)
			}
		}
		postureProviders = append(postureProviders, postureProvider)
	}

	return postureProviders, nil
}

func GetCloudComplianceNodesList(ctx context.Context, cloudProvider string, fw FetchWindow) (CloudNodeAccountsListResp, error) {
	driver, err := directory.Neo4jClient(ctx)
	if err != nil {
		return CloudNodeAccountsListResp{Total: 0}, err
	}

	session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	if err != nil {
		return CloudNodeAccountsListResp{Total: 0}, err
	}
	defer session.Close()

	tx, err := session.BeginTransaction()
	if err != nil {
		return CloudNodeAccountsListResp{Total: 0}, err
	}
	defer tx.Close()

	isOrgListing := false
	if cloudProvider == PostureProviderAWSOrg {
		cloudProvider = PostureProviderAWS
		isOrgListing = true
	}
	var res neo4j.Result
	if isOrgListing {
		res, err = tx.Run(`
		MATCH (m:Node{cloud_provider:$cloud_provider+'_org'}) -[:IS_CHILD]-> (n:Node{cloud_provider: $cloud_provider})
		WITH DISTINCT m.node_id AS node_id, m.node_name AS node_name, m.cloud_provider AS cloud_provider, m.updated_at AS updated_at
		OPTIONAL MATCH (n:Node{cloud_provider: $cloud_provider})<-[:SCANNED]-(s:CloudComplianceScan)-[:DETECTED]->(c:CloudComplianceResult)
		WITH node_id, node_name, cloud_provider, updated_at, COUNT(c) AS total_compliance_count
		OPTIONAL MATCH (n:Node{cloud_provider: $cloud_provider})<-[:SCANNED]-(s:CloudComplianceScan)-[:DETECTED]->(c1:CloudComplianceResult)
		WHERE c1.status IN ['ok', 'info', 'skip']
		RETURN node_id, node_name, cloud_provider, CASE WHEN total_compliance_count = 0 THEN 0.0 ELSE COUNT(c1.status)*100.0/total_compliance_count END AS compliance_percentage, updated_at
		ORDER BY updated_at`+fw.FetchWindow2CypherQuery(),
			map[string]interface{}{"cloud_provider": cloudProvider})
		if err != nil {
			return CloudNodeAccountsListResp{Total: 0}, err
		}
	} else {
		res, err = tx.Run(`
		MATCH (n:Node{cloud_provider: $cloud_provider}) 
		WITH DISTINCT n.node_id AS node_id, n.node_name AS node_name, n.cloud_provider AS cloud_provider, n.updated_at AS updated_at
		OPTIONAL MATCH (n:Node{cloud_provider: $cloud_provider})<-[:SCANNED]-(s:CloudComplianceScan)-[:DETECTED]->(c:CloudComplianceResult)
		WITH node_id, node_name, cloud_provider, updated_at, COUNT(c) AS total_compliance_count
		OPTIONAL MATCH (n:Node{cloud_provider: $cloud_provider})<-[:SCANNED]-(s:CloudComplianceScan)-[:DETECTED]->(c1:CloudComplianceResult)
		WHERE c1.status IN ['ok', 'info', 'skip']
		RETURN node_id, node_name, cloud_provider, CASE WHEN total_compliance_count = 0 THEN 0.0 ELSE COUNT(c1.status)*100.0/total_compliance_count END AS compliance_percentage, updated_at
		ORDER BY updated_at`+fw.FetchWindow2CypherQuery(),
			map[string]interface{}{"cloud_provider": cloudProvider})
		if err != nil {
			return CloudNodeAccountsListResp{Total: 0}, err
		}
	}

	recs, err := res.Collect()
	if err != nil {
		return CloudNodeAccountsListResp{Total: 0}, err
	}

	cloud_node_accounts_info := []CloudNodeAccountInfo{}
	for _, rec := range recs {
		tmp := CloudNodeAccountInfo{
			NodeId:               rec.Values[0].(string),
			NodeName:             rec.Values[1].(string),
			CloudProvider:        rec.Values[2].(string),
			CompliancePercentage: "0.00",
			Active:               "true",
		}
		cloud_node_accounts_info = append(cloud_node_accounts_info, tmp)
	}

	total := fw.Offset + len(cloud_node_accounts_info)
	var countRes neo4j.Result
	if isOrgListing {
		countRes, err = tx.Run(`
		MATCH (m:Node) -[:IS_CHILD]-> (n:Node{cloud_provider: $cloud_provider})
		RETURN COUNT(m)`,
			map[string]interface{}{"cloud_provider": cloudProvider})
	} else {
		countRes, err = tx.Run(`
		MATCH (n:Node {cloud_provider: $cloud_provider}) 
		RETURN COUNT(*)`,
			map[string]interface{}{"cloud_provider": cloudProvider})
	}

	countRec, err := countRes.Single()
	if err != nil {
		return CloudNodeAccountsListResp{CloudNodeAccountInfo: cloud_node_accounts_info, Total: total}, nil
	}

	total = int(countRec.Values[0].(int64))

	return CloudNodeAccountsListResp{CloudNodeAccountInfo: cloud_node_accounts_info, Total: total}, nil
}
