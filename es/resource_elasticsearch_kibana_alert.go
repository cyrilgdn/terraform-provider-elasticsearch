package es

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/olivere/elastic/uritemplates"

	elastic7 "github.com/olivere/elastic/v7"

	"github.com/phillbaker/terraform-provider-elasticsearch/kibana"
)

var minimalKibanaVersion, _ = version.NewVersion("7.7.0")
var notifyWhenKibanaVersion, _ = version.NewVersion("7.11.0")

func resourceElasticsearchKibanaAlert() *schema.Resource {
	return &schema.Resource{
		Create: resourceElasticsearchKibanaAlertCreate,
		Read:   resourceElasticsearchKibanaAlertRead,
		Update: resourceElasticsearchKibanaAlertUpdate,
		Delete: resourceElasticsearchKibanaAlertDelete,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				ForceNew:    true,
				Required:    true,
				Description: "",
			},
			"tags": {
				Type:        schema.TypeSet,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "",
			},
			"alert_type_id": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     ".index-threshold",
				Description: "The ID of the alert type that you want to call when the alert is scheduled to run, defaults to `.index-threshold`.",
			},
			"schedule": {
				Type:        schema.TypeList,
				MaxItems:    1,
				MinItems:    1,
				Optional:    true,
				Description: "",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"interval": {
							Type:     schema.TypeString,
							Required: true,
						},
					},
				},
			},
			"throttle": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "",
			},
			"notify_when": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The condition for throttling the notification: `onActionGroupChange`, `onActiveAlert`, or `onThrottleInterval`. Only available in Kibana >= 7.11",
			},
			"enabled": {
				Type:        schema.TypeBool,
				Default:     true,
				Optional:    true,
				Description: "",
			},
			"consumer": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "alerts",
				Description: "The name of the application that owns the alert. This name has to match the Kibana Feature name, as that dictates the required RBAC privileges. Defaults to `alerts`.",
			},
			"conditions": {
				Type:        schema.TypeSet,
				Required:    true,
				MaxItems:    1,
				MinItems:    1,
				Description: "The conditions under which the alert is active, they create an expression to be evaluated by the alert type executor. These parameters are passed to the executor `params`. There may be specific attributes for different alert types.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"threshold_comparator": {
							Type:     schema.TypeString,
							Required: true,
						},
						"time_window_size": {
							Type:     schema.TypeInt,
							Required: true,
						},
						"time_window_unit": {
							Type:     schema.TypeString,
							Required: true,
						},
						"term_size": {
							Type:     schema.TypeInt,
							Optional: true,
						},
						"time_field": {
							Type:     schema.TypeString,
							Required: true,
						},
						"group_by": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"aggregation_field": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"aggregation_type": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"term_field": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"index": {
							Type:        schema.TypeSet,
							Required:    true,
							MinItems:    1,
							Elem:        &schema.Schema{Type: schema.TypeString},
							Description: "",
						},
						"threshold": {
							Type:        schema.TypeSet,
							Required:    true,
							MinItems:    1,
							Elem:        &schema.Schema{Type: schema.TypeInt},
							Description: "",
						},
					},
				},
			},
			"actions": {
				Type:        schema.TypeSet,
				Optional:    true,
				Description: "",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"group": {
							Type:     schema.TypeString,
							Optional: true,
							Default:  "default",
						},
						"id": {
							Type:     schema.TypeString,
							Required: true,
						},
						"action_type_id": {
							Type:     schema.TypeString,
							Required: true,
						},
						"params": {
							Type:     schema.TypeMap,
							Optional: true,
						},
					},
				},
			},
		},
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		Description: "Alerts allow you to define rules to detect conditions and trigger actions when those conditions are met. Alerts work by running checks on a schedule to detect conditions. When a condition is met, the alert tracks it as an alert instance and responds by triggering one or more actions. Actions typically involve interaction with Kibana services or third party integrations. For more see the [docs](https://www.elastic.co/guide/en/kibana/current/alerting-getting-started.html).",
	}
}

func resourceElasticsearchKibanaAlertCreate(d *schema.ResourceData, meta interface{}) error {
	err := resourceElasticsearchKibanaAlertCheckVersion(meta)
	if err != nil {
		return err
	}

	id, err := resourceElasticsearchPostKibanaAlert(d, meta)
	if err != nil {
		return err
	}

	log.Printf("[INFO] Kibana Alert (%s) created", id)
	d.SetId(id)

	return nil
}

func resourceElasticsearchKibanaAlertRead(d *schema.ResourceData, meta interface{}) error {
	err := resourceElasticsearchKibanaAlertCheckVersion(meta)
	if err != nil {
		return err
	}

	id := d.Id()
	spaceID := ""

	var alert kibana.Alert

	esClient, err := getKibanaClient(meta.(*ProviderConf))
	if err != nil {
		return err
	}

	switch client := esClient.(type) {
	case *elastic7.Client:
		alert, err = kibanaGetAlert(client, id, spaceID)
	default:
		err = fmt.Errorf("Kibana Alert endpoint only available from Kibana >= 7.7, got version < 7.0.0")
	}

	if err != nil {
		if elastic7.IsNotFound(err) {
			log.Printf("[WARN] Kibana Alert (%s) not found, removing from state", id)
			d.SetId("")
			return nil
		}

		return err
	}

	schedule := make([]map[string]interface{}, 0, 1)
	schedule = append(schedule, map[string]interface{}{"interval": alert.Schedule.Interval})

	ds := &resourceDataSetter{d: d}
	ds.set("name", alert.Name)
	ds.set("tags", alert.Tags)
	ds.set("alert_type_id", alert.AlertTypeID)
	ds.set("schedule", schedule)
	ds.set("throttle", alert.Throttle)
	ds.set("notify_when", alert.NotifyWhen)
	ds.set("enabled", alert.Enabled)
	ds.set("consumer", alert.Consumer)
	ds.set("conditions", flattenKibanaAlertConditions(alert.Params))
	// ds.set("actions", alert.Actions) // TODO

	return ds.err
}

func resourceElasticsearchKibanaAlertUpdate(d *schema.ResourceData, meta interface{}) error {
	err := resourceElasticsearchKibanaAlertCheckVersion(meta)
	if err != nil {
		return err
	}

	return resourceElasticsearchPutKibanaAlert(d, meta)
}

func resourceElasticsearchKibanaAlertDelete(d *schema.ResourceData, meta interface{}) error {
	err := resourceElasticsearchKibanaAlertCheckVersion(meta)
	if err != nil {
		return err
	}

	id := d.Id()
	spaceID := ""

	kibanaClient, err := getKibanaClient(meta.(*ProviderConf))
	if err != nil {
		return err
	}

	switch client := kibanaClient.(type) {
	case *elastic7.Client:
		err = kibanaDeleteAlert(client, id, spaceID)
	default:
		err = fmt.Errorf("Kibana Alert endpoint only available from ElasticSearch >= 7.7, got version < 7.0.0")
	}

	if err != nil {
		return err
	}
	d.SetId("")
	return nil
}

func resourceElasticsearchPostKibanaAlert(d *schema.ResourceData, meta interface{}) (string, error) {
	spaceID := ""

	kibanaClient, err := getKibanaClient(meta.(*ProviderConf))
	if err != nil {
		return "", err
	}

	alertSchedule := kibana.AlertSchedule{}
	schedule := d.Get("schedule").([]interface{})
	if len(schedule) > 0 {
		scheduleEntry := schedule[0].(map[string]interface{})
		alertSchedule.Interval = scheduleEntry["interval"].(string)
	}
	actions, err := expandKibanaActionsList(d.Get("actions").(*schema.Set).List())
	if err != nil {
		return "", err
	}

	tags := expandStringList(d.Get("tags").(*schema.Set).List())

	conditions := d.Get("conditions").(*schema.Set).List()[0].(map[string]interface{})

	alert := kibana.Alert{
		Name:        d.Get("name").(string),
		Tags:        tags,
		AlertTypeID: d.Get("alert_type_id").(string),
		Schedule:    alertSchedule,
		Throttle:    d.Get("throttle").(string),
		Enabled:     d.Get("enabled").(bool),
		Consumer:    d.Get("consumer").(string),
		Params:      expandKibanaAlertConditions(conditions),
		Actions:     actions,
	}

	version, _ := resourceElasticsearchKibanaGetVersion(meta)
	if version.GreaterThanOrEqual(notifyWhenKibanaVersion) {
		alert.NotifyWhen = d.Get("notify_when").(string)
	}

	var id string
	switch client := kibanaClient.(type) {
	case *elastic7.Client:
		id, err = kibanaPostAlert(client, spaceID, alert)
	default:
		err = fmt.Errorf("Kibana Alert endpoint only available from ElasticSearch >= 7.7, got version < 7.0.0")
	}

	return id, err
}

func expandKibanaActionsList(resourcesArray []interface{}) ([]kibana.AlertAction, error) {
	actions := make([]kibana.AlertAction, 0, len(resourcesArray))
	for _, resource := range resourcesArray {
		data, ok := resource.(map[string]interface{})
		if !ok {
			return actions, fmt.Errorf("Error asserting data: %+v, %T", resource, resource)
		}
		action := kibana.AlertAction{
			ID:           data["id"].(string),
			Group:        data["group"].(string),
			ActionTypeId: data["action_type_id"].(string),
			Params:       data["params"].(map[string]interface{}),
		}
		actions = append(actions, action)
	}

	return actions, nil
}

func expandKibanaAlertConditions(raw map[string]interface{}) map[string]interface{} {
	conditions := make(map[string]interface{})

	// convert cases
	for k := range raw {
		camelCasedKey := toCamelCase(k, false)
		conditions[camelCasedKey] = raw[k]
		if camelCasedKey != k {
			delete(conditions, k)
		}
	}

	// override nested objects
	conditions["index"] = raw["index"].(*schema.Set).List()
	conditions["threshold"] = raw["threshold"].(*schema.Set).List()

	// convert abbreviated fields
	conditions["aggField"] = conditions["aggregationField"]
	delete(conditions, "aggregationField")
	conditions["aggType"] = conditions["aggregationType"]
	delete(conditions, "aggregationType")

	return conditions
}

func flattenKibanaAlertConditions(raw map[string]interface{}) []map[string]interface{} {
	conditions := make(map[string]interface{})

	// convert cases
	for k := range raw {
		underscoredKey := toUnderscore(k)
		conditions[underscoredKey] = raw[k]
		if underscoredKey != k {
			delete(conditions, k)
		}
	}
	log.Printf("[INFO] expandKibanaAlertConditions: %+v", conditions)
	// override nested objects
	conditions["index"] = flattenInterfaceSet(conditions["index"].([]interface{}))
	conditions["threshold"] = flattenFloatSet(conditions["threshold"].([]interface{}))

	// convert abbreviated fields
	conditions["aggregation_field"] = conditions["agg_field"]
	delete(conditions, "agg_field")
	conditions["aggregation_type"] = conditions["agg_type"]
	delete(conditions, "agg_type")

	return []map[string]interface{}{conditions}
}

func resourceElasticsearchPutKibanaAlert(d *schema.ResourceData, meta interface{}) error {
	return nil
}

func resourceElasticsearchKibanaGetVersion(meta interface{}) (*version.Version, error) {
	esClient, err := getClient(meta.(*ProviderConf))
	if err != nil {
		return nil, err
	}

	switch client := esClient.(type) {
	case *elastic7.Client:
		return elastic7GetVersion(client)
	default:
		return nil, fmt.Errorf("Kibana Alert endpoint only available from ElasticSearch >= 7.7, got version < 7.0.0")
	}
}

func resourceElasticsearchKibanaAlertCheckVersion(meta interface{}) error {
	elasticVersion, err := resourceElasticsearchKibanaGetVersion(meta)
	if err != nil {
		return err
	}

	if elasticVersion.LessThan(minimalKibanaVersion) {
		return fmt.Errorf("Kibana Alert endpoint only available from ElasticSearch >= 7.7, got version %s", elasticVersion.String())
	}

	return err
}

func kibanaGetAlert(client *elastic7.Client, id, spaceID string) (kibana.Alert, error) {
	path, err := uritemplates.Expand("/api/alerts/alert/{id}", map[string]string{
		"id": id,
	})
	if err != nil {
		return kibana.Alert{}, fmt.Errorf("error building URL path for alert: %+v", err)
	}

	var body json.RawMessage
	var res *elastic7.Response
	res, err = client.PerformRequest(context.TODO(), elastic7.PerformRequestOptions{
		Method: "GET",
		Path:   path,
	})
	body = res.Body

	if err != nil {
		return kibana.Alert{}, err
	}

	alert := new(kibana.Alert)
	if err := json.Unmarshal(body, alert); err != nil {
		return *alert, fmt.Errorf("error unmarshalling alert body: %+v: %+v", err, body)
	}

	return *alert, nil
}

func kibanaPostAlert(client *elastic7.Client, spaceID string, alert kibana.Alert) (string, error) {
	path, err := uritemplates.Expand("/api/alerts/alert", map[string]string{})
	if err != nil {
		return "", fmt.Errorf("error building URL path for alert: %+v", err)
	}

	body, err := json.Marshal(alert)
	if err != nil {
		log.Printf("[INFO] kibanaPostAlert: %+v %+v %+v", path, alert, err)
		return "", fmt.Errorf("Body Error: %s", err)
	}

	var res *elastic7.Response
	res, err = client.PerformRequest(context.TODO(), elastic7.PerformRequestOptions{
		Method: "POST",
		Path:   path,
		Body:   string(body[:]),
	})

	if err != nil {
		log.Printf("[INFO] kibanaPostAlert: %+v %+v %+v", path, alert, string(body[:]))
		return "", err
	}

	if err := json.Unmarshal(res.Body, &alert); err != nil {
		return "", fmt.Errorf("error unmarshalling alert body: %+v: %+v", err, body)
	}

	return alert.ID, nil
}

func kibanaDeleteAlert(client *elastic7.Client, id, spaceID string) error {
	path, err := uritemplates.Expand("/api/alerts/alert/{id}", map[string]string{
		"id": id,
	})
	if err != nil {
		return fmt.Errorf("error building URL path for alert: %+v", err)
	}

	_, err = client.PerformRequest(context.TODO(), elastic7.PerformRequestOptions{
		Method: "DELETE",
		Path:   path,
	})

	if err != nil {
		return err
	}

	return nil
}
