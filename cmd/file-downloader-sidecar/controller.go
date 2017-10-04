package main

import (
	"fmt"
	"time"
	"log"
	"io/ioutil"
	"os"
	"errors"
	"net/http"
	"io"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

type Controller struct {
	indexer  cache.Indexer
	queue    workqueue.RateLimitingInterface
	informer cache.Controller
	files    map[string]string
	filePath string
}

func NewController(client *kubernetes.Clientset, namespace string, configMapName string, filePath string) *Controller {
	configmapListWatcher := cache.NewListWatchFromClient(client.CoreV1().RESTClient(), "configmaps", namespace, fields.OneTermEqualSelector("metadata.name", configMapName))

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	indexer, informer := cache.NewIndexerInformer(configmapListWatcher, &v1.ConfigMap{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
	}, cache.Indexers{})

	files := make(map[string]string)

	return &Controller{
		informer: informer,
		indexer:  indexer,
		queue:    queue,
		files:    files,
		filePath: filePath,
	}
}

func (c *Controller) Run(threadiness int, stopCh chan struct{}) {
	defer runtime.HandleCrash()

	// Let the workers stop when we are done
	defer c.queue.ShutDown()
	log.Print("Starting File Downlaoder Sidecar controller")

	go c.informer.Run(stopCh)

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
	log.Print("Stopping File Downlaoder Sidecar controller")
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, shutdown := c.queue.Get()

	if shutdown {
		return false
	}

	defer c.queue.Done(key)

	// Invoke the method containing the business logic
	err := c.syncFiles(key.(string))

	// Handle the error if something went wrong during the execution of the business logic
	c.handleErr(err, key)

	// return true to continue the loop
	return true
}

// syncFiles is the business logic of the controller.
func (c *Controller) syncFiles(key string) error {
	obj, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		log.Printf("Fetching config map with key %s from store failed with %v", key, err)
		return err
	}

	files := make(map[string]string)

	if !exists {
		log.Printf("Config map %s does not exist anymore\n", key)
	} else {
		log.Printf("Received event for config map %s\n", obj.(*v1.ConfigMap).GetName())

		for key, value := range obj.(*v1.ConfigMap).Data {
			log.Printf("    %s=%s\n", key, value)
			files[key] = value
		}
	}

	filessToDownload, filessToDelete, err := c.diffPlugins(files)

	deleteErr := c.deletePlugins(filessToDelete)
	downloadErr := c.downloadPlugins(filessToDownload)

	// Let first finish both downloads and deletions before throwing the error
	// Thanks to this one file will not block all others
	if deleteErr != nil {
		return deleteErr
	}

	if downloadErr != nil {
		return downloadErr
	}

	return nil
}

func (c *Controller) diffPlugins(downloads map[string]string) (filesToDownload map[string]string, filesToDelete []string, err error) {
	filesToDownload = make(map[string]string)

	files, err := ioutil.ReadDir(c.filePath)
	if err != nil {
		log.Printf("Failed to list downloads in %s: %v", c.filePath, err)
		return
	}

	for _, file := range files {
		if _, ok := downloads[file.Name()]; !ok {
			log.Printf("Plugin %s found on disk but not in downloads list => should be deleted", file.Name())
			filesToDelete = append(filesToDelete, file.Name())
		}
	}

	for fileName, url := range downloads {
		found := false

		for _, file := range files {
			if file.Name() == fileName {
				found = true
			}
		}

		if !found {
			log.Printf("Plugin %s found in downloads list but not on disk => should be downloaded", fileName)
			filesToDownload[fileName] = url
		}
	}

	return
}

func (c *Controller) deletePlugins(filesToDelete []string) error {
	hasError := false

	for _, file := range filesToDelete {
		log.Printf("Deleting file %s", file)
		err := os.Remove(c.filePath + "/" + file)

		if err != nil {
			hasError = true
			log.Printf("Failed to delete file %s: %v", file, err)
		}
	}

	if hasError {
		log.Print("Some files failed to delete")
		return errors.New("Failed to delete all files listed for deletion")
	} else {
		return nil
	}
}

func (c *Controller) downloadPlugins(filessToDownload map[string]string) error {
	hasError := false

	for file, url := range filessToDownload {
		log.Printf("Downloading file %s from %s", file, url)

		outputFile, err := os.Create(c.filePath + "/" + file + ".tmp")
		if err != nil  {
			hasError = true
			log.Printf("Failed to created new file file %s: %v", c.filePath+ "/" + file + ".tmp", err)
			continue
		}

		defer outputFile.Close()

		downloadFile, err := http.Get(url)
		if err != nil {
			hasError = true
			log.Printf("Failed to start the download from %s: %v", url, err)
			continue
		}

		defer downloadFile.Body.Close()

		// Writer the body to file
		_, err = io.Copy(outputFile, downloadFile.Body)
		if err != nil  {
			hasError = true
			log.Printf("Failed to download the file from %s to %s: %v", url, c.filePath+ "/" + file + ".tmp", err)
			continue
		}

		err = os.Rename(c.filePath+ "/" + file + ".tmp", c.filePath+ "/" + file)
		if err != nil {
			hasError = true
			log.Printf("Failed to rename file from %s to %s: %v", c.filePath+ "/" + file + ".tmp", c.filePath+ "/" + file, err)
			continue
		}
	}

	if hasError {
		log.Print("Some files failed to download")
		return errors.New("Failed to download all files listed for deletion")
	} else {
		return nil
	}
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		// No error -> forget the key
		c.queue.Forget(key)
		return
	}

	// Backoff and try later
	runtime.HandleError(err)
	c.queue.AddRateLimited(key)
	return
}



