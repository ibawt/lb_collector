all:
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o lb_collector
	docker build -t lb_collector:latest -t gcr.io/shopify-docker-images/lb_collector:latest .
	gcloud docker -- push gcr.io/shopify-docker-images/lb_collector
