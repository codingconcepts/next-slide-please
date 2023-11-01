# next-slide-please
Because no one should have to ask someone else to progress their slides.

### Installation

Download the tar file that corresponds to your OS (currently only macos is supported) from the [Releases](https://github.com/codingconcepts/next-slide-please/releases/latest) page, and extract it as follows:

``` sh
tar -xvf dg_[VERSION]_[OS].tar.gz
```

Move the resulting nps file into your PATH.

### Usage

The speaker (the person asking for the next slide), runs the app as follows. This will create a unique topic to share with the presenter:

``` sh
nsp speak

# Listening for [left | right] direction keys and publishing on "nZRw9s0Tv7", press ctrl+c to close.
```

The presenter (the person with the sides and being asked for the next slide), runs the app as follows. This will subscribe to the speaker's topic and propagate their commands:

``` sh
nsp present nZRw9s0Tv7

# Subscribed for [left | right] direction keys, press ctrl+c to close.
```

### Running locally

Deploy server

``` sh
fly launch --name next-slide-please --no-deploy
fly deploy
```

Test server

``` sh
nats context save local \
  --server nats://next-slide-please.fly.dev:4222 \
  --description 'fly.io' \
  --select 

nats sub next-slide-please.test
nats pub next-slide-please.test "test"
```