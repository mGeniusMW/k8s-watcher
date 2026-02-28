package main

import (
	"fmt"
	"path/filepath"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	config, _ := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	clientset, _ := kubernetes.NewForConfig(config)

	factory := informers.NewSharedInformerFactory(clientset, time.Second*30)
	
	podInformer := factory.Core().V1().Pods().Informer()

	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			fmt.Printf("NEW POD DETECTED: %v\n", obj)
		},
		DeleteFunc: func(obj interface{}) {
			fmt.Printf("POD DELETED: %v\n", obj)
		},
	})

	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)

	fmt.Println("Watcher started... waiting for changes in 'kind' cluster.")
	
	select {} 
}
