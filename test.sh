# This isn't an automated test
sudo docker build -f ./Dockerfile.test -t bradleychatha/prostagma:test .
sudo docker rm prostagma-test-server prostagma-test-client --force
sudo docker run -e PROSTAGMA_HOST=0.0.0.0:6969 \
                -e PROSTAGMA_SECRET=boobs \
                --network=host \
                --name=prostagma-test-server \
                -d \
                -t bradleychatha/prostagma:test prostagma server
sudo docker run -e PROSTAGMA_HOST=http://127.0.0.1:6969 \
                -e PROSTAGMA_SECRET=boobs \
                -e PROSTAGMA_TRIGGER=test \
                -e PROSTAGMA_SCRIPT=/build.yaml \
                -e PROSTAGMA_SHELL=/bin/sh \
                -v $PWD/test.yaml:/build.yaml \
                --network=host \
                --name=prostagma-test-client \
                -d \
                -t bradleychatha/prostagma:test prostagma client
curl -X POST -d '{"secret": "boobs", "trigger": "test"}' http://127.0.0.1:6969/trigger -v