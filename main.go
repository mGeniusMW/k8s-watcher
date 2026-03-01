package main

import (
	"fmt"
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
	Name              string 'json:"name"'
	Namespace         string 
	Status            string
	ContainerStatuses []string
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

func AlertIfNeeded(event string, k8sPod *k8sApi.Pod) {
	rawAge := PodAge(k8sPod)
	if rawAge < 30*time.Second {
		return
	}

	myLocalPod := BuildLocalPod(k8sPod)
	if myLocalPod.Status == "Running" {
		return
	}

	ageText := FormatAgeShort(rawAge)
	fmt.Printf("Fire Alert!! %s %s/%s - Age: %s - Status: %s - Container States: %v\n",
		event, myLocalPod.Namespace, myLocalPod.Name, ageText, myLocalPod.Status, myLocalPod.ContainerStatuses)
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

	factory := informers.NewSharedInformerFactory(clientset, time.Second*30)

	LocalPodInformer := factory.Core().V1().Pods().Informer()
	LocalPodInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			k8sPod, ok := ExtractPod(obj)
			if !ok {
				return
			}
			AlertIfNeeded("POD ADDED:", k8sPod)
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

			AlertIfNeeded("POD UPDATED:", newPod)
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
