LEAK ?= read
FIX  ?= false
MODE ?= silent

run:
	LEAK=$(LEAK) FIX=$(FIX) MODE=$(MODE) docker compose up --build

inspect:
	docker inspect --format "OOMKilled={{.State.OOMKilled}} Restarts={{.RestartCount}}" $$(docker compose ps -aq server)

plot:
	python3 scripts/plot.py metrics/$(LEAK)-false.csv metrics/$(LEAK)-true.csv -o metrics/$(LEAK).png

test:
	go test -race ./...

clean:
	rm -f metrics/*.csv metrics/*.png