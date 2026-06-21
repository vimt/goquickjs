-- build __N__ small strings, join them, then rolling-hash the char codes.
local function main()
  local N = __N__
  local parts = {}
  for i = 0, N - 1 do
    parts[#parts + 1] = "item" .. (i % 1000)
  end
  local s = table.concat(parts, ",")
  local sum = 0
  for i = 1, #s do
    sum = (sum * 31 + string.byte(s, i)) % 1000000007
  end
  return sum
end
result = string.format("%d", main())
