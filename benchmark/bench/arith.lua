-- nested integer loops + modular arithmetic. __N__ is injected by the host.
local function main()
  local N = __N__
  local acc = 0
  for i = 0, N - 1 do
    for j = 0, 99 do
      acc = (acc + i * 31 + j * 7) % 1000000007
    end
  end
  return acc
end
result = string.format("%d", main())
