# nested integer loops + modular arithmetic. __N__ is injected by the host.
def main():
    N = __N__
    acc = 0
    for i in range(N):
        for j in range(100):
            acc = (acc + i * 31 + j * 7) % 1000000007
    return acc

result = str(main())
