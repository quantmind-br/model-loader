#include "llama-lazy-reader.h"
#include <cassert>
#include <future>
#include <iostream>
#include <sys/wait.h>
#include <sys/resource.h>
int main() {
 for (auto type : {GGML_TYPE_F32, GGML_TYPE_Q8_0, GGML_TYPE_IQ4_XS}) {
  const int dim=256, count=101;
  const size_t row=ggml_row_size(type,dim), base=128;
  std::vector<unsigned char> bytes(base+row*count,0);
  if(type==GGML_TYPE_F32) for(int r=0;r<count;r++)for(int k=0;k<dim;k++) {float f=float(r*dim+k);memcpy(bytes.data()+base+r*row+k*4,&f,4);}
  char path[]="/tmp/reader-test-XXXXXX"; int fd=mkstemp(path);assert(fd>=0);unlink(path);
  assert(write(fd,bytes.data(),bytes.size())==(ssize_t)bytes.size());
  llama_lazy_reader reader(fd,base,row,count,8,type,dim);
  auto check=[&](){std::vector<int32_t> ids(1024);for(int i=0;i<1024;i++)ids[i]=(i*37)%count;std::vector<float> out(ids.size()*dim),ref(dim);reader.gather(ids.data(),ids.size(),out.data());for(size_t i=0;i<ids.size();i++){const void* src=bytes.data()+base+ids[i]*row;if(type==GGML_TYPE_F32)memcpy(ref.data(),src,row);else ggml_get_type_traits(type)->to_float(src,ref.data(),dim);assert(memcmp(ref.data(),out.data()+i*dim,dim*4)==0);}reader.gather(nullptr,0,nullptr);};
  check();auto a=std::async(std::launch::async,check);auto b=std::async(std::launch::async,check);a.get();b.get();
  assert(ftruncate(fd,base+row*2)==0);bool caught=false;int32_t id=50;std::vector<float> out(dim);try{reader.gather(&id,1,out.data());}catch(const std::runtime_error&){caught=true;}assert(caught);
  std::cout<<ggml_type_name(type)<<": order, duplicates, concurrent gathers, empty, EOF OK\n";
 }
}
