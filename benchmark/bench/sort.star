# build an array via a MINSTD LCG, insertion-sort it, then rolling-hash the
# sorted values (order matters, index base does not). __N__ = array size.
def main():
    S = __N__
    seed = 12345
    arr = []
    for i in range(S):
        seed = (seed * 48271) % 2147483647
        arr.append(seed % 10000)
    for i in range(1, S):
        key = arr[i]
        j = i - 1
        for k in range(i):
            if j >= 0 and arr[j] > key:
                arr[j + 1] = arr[j]
                j -= 1
            else:
                break
        arr[j + 1] = key
    total = 0
    for i in range(S):
        total = (total * 31 + arr[i]) % 1000000007
    return total

result = str(main())
