# iterative fibonacci (mod P), repeated __N__ times. Tests tight scalar loops.
def main():
    reps = __N__
    total = 0
    for r in range(reps):
        a = 0
        b = 1
        for i in range(90):
            t = (a + b) % 1000000007
            a = b
            b = t
        total = (total + a) % 1000000007
    return total

result = str(main())
