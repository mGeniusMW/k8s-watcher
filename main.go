package main

import (
	"fmt"
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
	Name              string
	Namespace         string
	Status            string
	ContainerStatuses []string
}

func main() {
	config, _ := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	clientset, _ := kubernetes.NewForConfig(config)

	factory := informers.NewSharedInformerFactory(clientset, time.Second*30)

	LocalPodInformer := factory.Core().V1().Pods().Informer()
	// pod.Status.ContainerStatuses is where info such as CrashloopBackOff etc is stored
	LocalPodInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			k8sPod, ok := obj.(*k8sApi.Pod)
			if !ok {
				return
			}

			// 1. Create a slice to hold our strings
			var statuses []string

			// 2. Loop through the K8s container statuses
			for _, cs := range k8sPod.Status.ContainerStatuses {
				// We check if the container is waiting (this is where CrashLoopBackOff lives)
				if cs.State.Waiting != nil {
					statuses = append(statuses, cs.State.Waiting.Reason)
				} else if cs.State.Running != nil {
					statuses = append(statuses, "Running")
				} else if cs.State.Terminated != nil {
					statuses = append(statuses, cs.State.Terminated.Reason)
				}
			}

			myLocalPod := LocalPod{
				Name:              k8sPod.Name,
				Namespace:         k8sPod.Namespace,
				Status:            string(k8sPod.Status.Phase),
				ContainerStatuses: statuses, // Now the types match! ([]string)
			}

			fmt.Printf("POD ADDED: %s/%s - Status: %s - Container States: %v\n",
				myLocalPod.Namespace, myLocalPod.Name, myLocalPod.Status, myLocalPod.ContainerStatuses)
		}, DeleteFunc: func(obj interface{}) {
			fmt.Printf("POD DELETED: %v\n", obj)
		},
	})

	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)

	fmt.Println("Watcher started... waiting for changes in 'kind' cluster.")

	select {}
}
