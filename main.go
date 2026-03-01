package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	k8sApi "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type LocalPod struct {
	Name              string   `json:"name"`
	Namespace         string   `json:"namespace"`
	Status            string   `json:"status"`
	ContainerStatuses []string `json:"containerStatuses"`
}

type Alert struct {
	Event string
	Pod   LocalPod
	Age   string
}

func PodAge(pod *k8sApi.Pod) time.Duration {
	if pod == nil {
		return 0
	}
	return time.Since(pod.CreationTimestamp.Time)
}

func FormatAgeShort(age time.Duration) string {
	if age < 0 {
		age = 0
	}

	seconds := int(age.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}

	minutes := int(age.Minutes())
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}

	hours := int(age.Hours())
	if hours < 24 {
		return fmt.Sprintf("%dh", hours)
	}

	days := hours / 24
	if days < 365 {
		return fmt.Sprintf("%dd", days)
	}

	years := days / 365
	return fmt.Sprintf("%dy", years)
}

func ExtractPod(obj interface{}) (*k8sApi.Pod, bool) {
	switch typedObj := obj.(type) {
	case *k8sApi.Pod:
		return typedObj, true
	case cache.DeletedFinalStateUnknown:
		pod, ok := typedObj.Obj.(*k8sApi.Pod)
		return pod, ok
	case *cache.DeletedFinalStateUnknown:
		pod, ok := typedObj.Obj.(*k8sApi.Pod)
		return pod, ok
	default:
		return nil, false
	}
}

func BuildLocalPod(k8sPod *k8sApi.Pod) LocalPod {
	var statuses []string

	for _, cs := range k8sPod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			statuses = append(statuses, cs.State.Waiting.Reason)
		} else if cs.State.Running != nil {
			statuses = append(statuses, "Running")
		} else if cs.State.Terminated != nil {
			statuses = append(statuses, cs.State.Terminated.Reason)
		}
	}

	return LocalPod{
		Name:              k8sPod.Name,
		Namespace:         k8sPod.Namespace,
		Status:            string(k8sPod.Status.Phase),
		ContainerStatuses: statuses,
	}
}

// AlertIfNeeded checks if an alert should be sent and if so, sends it to the channel (Producer)
func AlertIfNeeded(event string, k8sPod *k8sApi.Pod, alertChan chan Alert) {
	rawAge := PodAge(k8sPod)
	if rawAge < 30*time.Second {
		return
	}

	myLocalPod := BuildLocalPod(k8sPod)
	if myLocalPod.Status == "Running" {
		return
	}

	ageText := FormatAgeShort(rawAge)

	// Send alert to channel (non-blocking producer)
	alertChan <- Alert{
		Event: event,
		Pod:   myLocalPod,
		Age:   ageText,
	}
}

// worker processes alerts from the channel (Consumer)
func worker(alertChan chan Alert) {
	for alert := range alertChan {
		// Marshal the pod to JSON
		marshalled, err := json.Marshal(alert.Pod)
		if err != nil {
			log.Printf("Error marshalling pod to JSON: %v", err)
			continue
		}

		// Create HTTP POST request with JSON body
		req, err := http.NewRequest("POST", "http://localhost:8000/alerts", bytes.NewReader(marshalled))
		if err != nil {
			log.Printf("impossible to build request: %s", err)
			continue
		}

		// Set Content-Type header
		req.Header.Set("Content-Type", "application/json")

		// Create HTTP client and execute request
		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("error sending request: %s", err)
			continue
		}
		defer resp.Body.Close()

		// Check response status
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			fmt.Printf("%s %s/%s - Age: %s - Status: %s - Alert sent successfully\n",
				alert.Event, alert.Pod.Namespace, alert.Pod.Name, alert.Age, alert.Pod.Status)
		} else {
			log.Printf("alert failed for %s/%s - HTTP status: %d\n",
				alert.Pod.Namespace, alert.Pod.Name, resp.StatusCode)
		}
	}
}

func main() {
	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load kubeconfig: %v\n", err)
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	// Create buffered channel for alerts
	alertChan := make(chan Alert, 100)

	// Start worker goroutine to process alerts
	go worker(alertChan)

	factory := informers.NewSharedInformerFactory(clientset, time.Second*30)

	LocalPodInformer := factory.Core().V1().Pods().Informer()
	LocalPodInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			k8sPod, ok := ExtractPod(obj)
			if !ok {
				return
			}
			AlertIfNeeded("POD ADDED:", k8sPod, alertChan)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			newPod, ok := ExtractPod(newObj)
			if !ok {
				return
			}

			oldPod, ok := ExtractPod(oldObj)
			if ok && oldPod.ResourceVersion == newPod.ResourceVersion {
				return
			}

			AlertIfNeeded("POD UPDATED:", newPod, alertChan)
		},
		DeleteFunc: func(obj interface{}) {
			k8sPod, ok := ExtractPod(obj)
			if !ok {
				fmt.Printf("POD DELETED: unknown object type %T\n", obj)
				return
			}

			fmt.Printf("POD DELETED: %s/%s\n", k8sPod.Namespace, k8sPod.Name)
		},
	})

	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)

	fmt.Println("Watcher started... waiting for changes in 'kind' cluster.")

	select {}
}
