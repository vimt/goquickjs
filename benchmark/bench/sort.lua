-- build an array via a MINSTD LCG, insertion-sort it, then rolling-hash the
-- sorted values (order matters, index base does not). __N__ = array size.
local function main()
  local S = __N__
  local seed = 12345
  local arr = {}
  for i = 1, S do
    seed = (seed * 48271) % 2147483647
    arr[i] = seed % 10000
  end
  for i = 2, S do
    local key = arr[i]
    local j = i - 1
    for k = 1, i - 1 do
      if j >= 1 and arr[j] > key then
        arr[j + 1] = arr[j]
        j = j - 1
      else
        break
      end
    end
    arr[j + 1] = key
  end
  local sum = 0
  for i = 1, S do
    sum = (sum * 31 + arr[i]) % 1000000007
  end
  return sum
end
result = string.format("%d", main())
