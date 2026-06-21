# build __N__ small strings, join them, then rolling-hash the char codes.
def main():
    N = __N__
    parts = []
    for i in range(N):
        parts.append("item" + str(i % 1000))
    s = ",".join(parts)
    total = 0
    for i in range(len(s)):
        total = (total * 31 + ord(s[i])) % 1000000007
    return total

result = str(main())
