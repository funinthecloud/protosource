# protosource
Protocol buffers only eventsourcing

# Inspiration

* [Matt Ho](https://www.youtube.com/watch?v=B-reKkB8L5Q)
   * https://github.com/altairsix/eventsource
   * https://github.com/altairsix/eventsource-protobuf
* [Vance Long Will](https://github.com/VanceLongwill/eventsource)
 

# Reasoning
1. Microservices do not actually solve the problem, you are just as calcified as you ever were.
2. Protocol buffers solve quite a few long term maintenance issues.
3. I, too, hate managing anything outside of my code.
4. Others.

# Target
* Data storage in some sort of cost efficient store (that I do not have to manage).
* Trivial testing, even from a laptop when I am on the plane.
* As much as possible have all adapter code (lambda, interface methods, etc...) auto generated based on protocol buffers.
* I should only have to write the business logic for managing the commands, events, and the aggregate state.

# Technologies
1. [Protocol buffers](https://developers.google.com/protocol-buffers)
2. [Buf](https://buf.build)
3. Domain Modeling
4. EventSourcing
5. CQRS
