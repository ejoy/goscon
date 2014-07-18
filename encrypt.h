#ifndef encrypt_h
#define encrypt_h

#include <stdint.h>
#include <stddef.h>

// x: random
uint64_t exchange(uint64_t x);

// x: remote key
// y: self key
uint64_t secret(uint64_t x, uint64_t y);

/*
uint64_t powmodp(uint64_t a, uint64_t b);
*/

uint64_t randomint64();
uint64_t hmac(uint64_t x, uint64_t y);
uint64_t hash(const uint8_t* str, int sz);

uint64_t uint64_decode(const uint8_t* str, int sz);
void uint64_encode(uint64_t v, uint8_t* buf, int sz);
#endif

